package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
	"yanglei_blinder/logger"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	// "github.com/pion/rtcp"

	"github.com/pion/webrtc/v4"
)

type ConfRoom struct {
	Name                string
	PubPC               *webrtc.PeerConnection
	PubRemoteVideoTrack *webrtc.TrackRemote
	PubRemoteAudioTrack *webrtc.TrackRemote
	PubLocalAudioTrack  *webrtc.TrackLocalStaticRTP
	PubLocalAudioChan   chan *rtp.Packet
	SubLocalVideoTrack  map[string]*webrtc.TrackLocalStaticRTP
	SublocalAudioTrack  map[string]*webrtc.TrackLocalStaticRTP
	CreatedAt           time.Time
	PubQuit             bool
	IsPlayingFile       bool
}

type ConfInfo struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
}

var ConfRoomList = make(map[string]*ConfRoom, 0)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}
var webPort = ":9000"
var httpsPort = ":9443"

var recordPath = "./record"

func main() {
	http.HandleFunc("/ws", HandleWebSocket)
	fs := http.FileServer(http.Dir("./web"))
	http.Handle("/", fs)
	http.HandleFunc("/api/confInfo", HandleGetConfInfo) // 新增的 GET endpoint

	// 启动 HTTP 服务器
	go func() {
		log.Printf("Starting HTTP server at %s\n", webPort)
		log.Fatal(http.ListenAndServe(webPort, nil))
	}()

	// 启动 HTTPS 服务器
	go func() {
		log.Printf("Starting HTTPS server at %s\n", httpsPort)
		certFile := "./blinder.aiiyou.cn/aliyun/blinder.aiiyou.cn.pem" // 替换为你的证书路径
		keyFile := "./blinder.aiiyou.cn/aliyun/blinder.aiiyou.cn.key"  // 替换为你的私钥路径
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS10,
		}
		srv := &http.Server{
			Addr:      httpsPort,
			Handler:   nil,
			TLSConfig: tlsConfig,
		}
		log.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
	}()

	os.MkdirAll(recordPath, os.ModePerm)

	select {} // 阻止主 goroutine 退出
}

func HandleGetConfInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var confRooms []ConfInfo
	for _, room := range ConfRoomList {
		confRooms = append(confRooms, ConfInfo{
			Name:      room.Name,
			CreatedAt: room.CreatedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(confRooms)
}

func HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		logger.Error(err)
		return
	}
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error(err)
			break
		}
		logger.Infof("recv message:%s", string(message))

		var msg map[string]interface{}
		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Error(err)
			continue
		}
		msgCmd := msg["cmd"].(string)
		logger.Infof("msgCmd:%s", msgCmd)

		switch msgCmd {
		case "create":
			roomName := msg["roomName"].(string)
			logger.Infof("roomName:%s", roomName)
			if len(roomName) == 0 {
				logger.Errorf("roomName: %v is invlaid", roomName)
				continue
			}
			createdRoom, err := CreateConfRoom(roomName)
			if err != nil {
				logger.Error(err)
				continue
			}
			answerSdp, err := HandlePubOffer(msg["sdp"].(string), createdRoom)
			if err != nil {
				logger.Error(err)
				continue
			}
			jsonData, err := json.Marshal(map[string]string{"answer": answerSdp, "type": "answer"})
			if err != nil {
				logger.Error(err)
				continue
			}
			err = conn.WriteMessage(websocket.TextMessage, jsonData)
			if err != nil {
				logger.Error(err)
				continue
			}
		case "join":
			roomName := msg["roomName"].(string)
			logger.Info("roomName: %s", roomName)
			if len(roomName) == 0 {
				logger.Errorf("roomName: %s", roomName)
				continue
			}
			joinRoom, exists := ConfRoomList[roomName]
			if !exists {
				logger.Errorf("joinRoom: %s is not existed")
				continue
			}

			answerSdp, err := HandleSubOffer(msg["userId"].(string), msg["sdp"].(string), joinRoom)
			if err != nil {
				logger.Error(err)
				continue
			}
			jsonData, err := json.Marshal(map[string]string{"answer": answerSdp, "type": "answer"})
			if err != nil {
				logger.Error(err)
				continue
			}
			err = conn.WriteMessage(websocket.TextMessage, jsonData)
			if err != nil {
				logger.Error(err)
				continue
			}
		case "control":
			roomName := msg["roomName"].(string)
			if len(roomName) == 0 {
				http.Error(w, "Invalid room name", http.StatusBadRequest)
				return
			}
			joinRoom, exists := ConfRoomList[roomName]
			if !exists {
				http.Error(w, "Room does not exist", http.StatusNotFound)
				return
			}

			cmdDetail := msg["cmdDetail"].(string)
			go FFmpegFileToRTPPackets(fmt.Sprintf("audio/%s.ogg", cmdDetail), joinRoom)

		default:
			logger.Errorf("invalid msgCmd: %s, msg:%v", msgCmd, msg)

		}
	}

}

func HandleSubOffer(userName string, offer string, confRoom *ConfRoom) (string, error) {
	logger.Info("handleSubOffer comming...")
	recvOnlyOffer := webrtc.SessionDescription{}
	err := decode(offer, &recvOnlyOffer)
	if err != nil {
		return "", err
	}
	logger.Info("-----------Sub Recv Sdp Offer------------------")
	logger.Info(recvOnlyOffer)
	logger.Info("-------------------------------------------")

	// Create a new PeerConnection
	peerConnectionConfig := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}
	peerConnection, err := webrtc.NewPeerConnection(peerConnectionConfig)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	today := time.Now().Format("2006-01-02")
	os.MkdirAll(fmt.Sprintf("%s/%s", recordPath, today), os.ModePerm)
	recordFileName := fmt.Sprintf("%s/%s/%s_sub_%v", recordPath, today, confRoom.Name, confRoom.CreatedAt.Format("15_04_05"))

	subRecordSaver := newWebmSaver(recordFileName)
	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			logger.Infof("sub remoteTrack codec MimeType: %v, ClockRate:%v, channels:%v ", remoteTrack.Codec().MimeType, remoteTrack.Codec().ClockRate, remoteTrack.Codec().Channels)

			go func() {
				logger.Info("Sub Audio Track")
				defer logger.Info("Sub Audio Track end.")
				for {
					rtpPacket, _, readErr := remoteTrack.ReadRTP()
					if readErr != nil {
						logger.Error(readErr)
						return
					}
					// logger.Infof("Sub audio len:%v, header:%v", len(rtpPacket.Payload), rtpPacket.Header)

					subRecordSaver.mu.Lock()
					subRecordSaver.PushOpus(rtpPacket)
					subRecordSaver.mu.Unlock()

					if confRoom.PubQuit {
						logger.Warn("pub quit,so peerConnection will be close")
						return
					}
					if !confRoom.IsPlayingFile {
						select {
						case confRoom.PubLocalAudioChan <- rtpPacket:
						default:

						}
					}
				}
			}()

		}
	})

	peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		if is == webrtc.ICEConnectionStateDisconnected || is == webrtc.ICEConnectionStateFailed {
			logger.Warn("peerConnection will be close")
			delete(confRoom.SubLocalVideoTrack, userName)
			delete(confRoom.SublocalAudioTrack, userName)
			subRecordSaver.Close()
			peerConnection.Close()
		}
	})

	//Video track
	if confRoom == nil || confRoom.PubRemoteVideoTrack == nil {
		logger.Error("PubRemoteVideoTrack is nil")
		return "", errors.New("PubRemoteVideoTrack is nil")

	}
	localVideoTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(confRoom.PubRemoteVideoTrack.Codec().RTPCodecCapability, "video", "pion")
	if newTrackErr != nil {
		logger.Error(newTrackErr)
		return "", newTrackErr
	}

	rtpVideoSender, err := peerConnection.AddTrack(localVideoTrack)
	if err != nil {
		logger.Error(err)
		return "", err
	}
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpVideoSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	//Audio track
	localAudioTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(confRoom.PubRemoteAudioTrack.Codec().RTPCodecCapability, "audio", "pion")
	if newTrackErr != nil {
		logger.Error(newTrackErr)
		return "", newTrackErr
	}

	rtpAudioSender, err := peerConnection.AddTrack(localAudioTrack)
	if err != nil {
		logger.Error(err)
		return "", err
	}
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpAudioSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Set the remote SessionDescription
	err = peerConnection.SetRemoteDescription(recvOnlyOffer)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	confRoom.SubLocalVideoTrack[userName] = localVideoTrack
	confRoom.SublocalAudioTrack[userName] = localAudioTrack

	logger.Info("handleSubOffer end, will return answer sdp:\n")
	logger.Info(peerConnection.LocalDescription())
	// Get the LocalDescription and take it to base64 so we can paste in browser
	return encode(peerConnection.LocalDescription()), nil

}

func HandlePubOffer(offer string, confRoom *ConfRoom) (string, error) {

	logger.Info("handlePubOffer comming...")

	offerSD := webrtc.SessionDescription{}
	err := decode(offer, &offerSD)
	if err != nil {
		return "", err
	}
	logger.Info("-----------Recv Sdp Offer------------------")
	logger.Info(offerSD)
	logger.Info("-------------------------------------------")

	peerConnectionConfig := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.l.google.com:19302"},
			},
		},
	}

	m := &webrtc.MediaEngine{}

	if err = m.RegisterDefaultCodecs(); err != nil {
		logger.Error(err)
		return "", err
	}
	i := &interceptor.Registry{}
	if err = webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		logger.Error(err)
		return "", err
	}
	intervalPliFactory, err := intervalpli.NewReceiverInterceptor()
	if err != nil {
		logger.Error(err)
		return "", err
	}
	i.Add(intervalPliFactory)
	peerConnection, err := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i)).NewPeerConnection(peerConnectionConfig)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	if _, err = peerConnection.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo); err != nil {
		logger.Error(err)
		return "", err
	}

	localAudioTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus}, "audio", "pion")
	if err != nil {
		logger.Error(err)
		return "", err
	}
	rtpSender, err := peerConnection.AddTrack(localAudioTrack)
	if err != nil {
		logger.Error(err)
		return "", err
	}
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	confRoom.PubLocalAudioTrack = localAudioTrack

	today := time.Now().Format("2006-01-02")
	os.MkdirAll(fmt.Sprintf("%s/%s", recordPath, today), os.ModePerm)
	recordFileName := fmt.Sprintf("%s/%s/%s_pub_%v", recordPath, today, confRoom.Name, confRoom.CreatedAt.Format("15_04_05"))
	pubRecordSaver := newWebmSaver(recordFileName)

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
		logger.Info("OnTrack comming....", remoteTrack)

		if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			logger.Infof("remoteTrack codec MimeType: %v, ClockRate:%v, channels:%v ", remoteTrack.Codec().MimeType, remoteTrack.Codec().ClockRate, remoteTrack.Codec().Channels)
			go PubLocalAudioWrite(confRoom.PubLocalAudioChan, confRoom, uint8(remoteTrack.Codec().PayloadType))
			go func() {
				defer logger.Info("pub audio track quit")
				logger.Info("pub auido track")
				codec := remoteTrack.Codec()
				logger.Infof("pub audio codec:%v", codec)
				confRoom.PubRemoteAudioTrack = remoteTrack

				for {
					rtpPacketA, _, readErr := remoteTrack.ReadRTP()
					if readErr != nil {
						logger.Error(readErr)
						return
					}
					pubRecordSaver.mu.Lock()
					pubRecordSaver.PushOpus(rtpPacketA)
					pubRecordSaver.mu.Unlock()
					for _, localTrack := range confRoom.SublocalAudioTrack {
						err := localTrack.WriteRTP(rtpPacketA)
						if err != nil && !errors.Is(err, io.EOF) {
							logger.Error(err)
							continue
						}
					}

				}
			}()
		}
		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			go func() {
				defer logger.Info("pub video track quit")
				logger.Info("pub video track")
				codec := remoteTrack.Codec()
				logger.Infof("pub video codec:%v", codec)
				confRoom.PubRemoteVideoTrack = remoteTrack
				errSend := peerConnection.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{MediaSSRC: uint32(remoteTrack.SSRC())}})
				if errSend != nil {
					logger.Error(errSend)
				}
				snapShotChan := make(chan *rtp.Packet)
				defer close(snapShotChan)
				go func() {
					Snapshot(snapShotChan, recordPath, confRoom.Name)
				}()

				// 创建或打开音频录制文件
				for {
					rtpPacketV, _, readErr := remoteTrack.ReadRTP()
					if readErr != nil {
						logger.Error(readErr)
						return
					}

					for _, localTrack := range confRoom.SubLocalVideoTrack {
						err := localTrack.WriteRTP(rtpPacketV)
						if err != nil && !errors.Is(err, io.EOF) {
							logger.Error(err)
							break
						}
					}
					switch codec.MimeType {
					case webrtc.MimeTypeVP8:
						pubRecordSaver.mu.Lock()
						pubRecordSaver.PushVP8(rtpPacketV)
						pubRecordSaver.mu.Unlock()
					}
					select {
					case snapShotChan <- rtpPacketV:
					default:
					}

				}

			}()
		}

	})

	peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		if is == webrtc.ICEConnectionStateFailed || is == webrtc.ICEConnectionStateDisconnected || is == webrtc.ICEConnectionStateClosed {
			confRoom.PubQuit = true
			time.Sleep(10*time.Microsecond)
			peerConnection.Close()
			pubRecordSaver.Close()
			close(confRoom.PubLocalAudioChan)
			delete(ConfRoomList, confRoom.Name)
		}
	})

	// Set the remote SessionDescription
	err = peerConnection.SetRemoteDescription(offerSD)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		logger.Error(err)
		return "", err
	}
	confRoom.PubPC = peerConnection

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConnection)

	// Sets the LocalDescription, and starts our UDP listeners
	err = peerConnection.SetLocalDescription(answer)
	if err != nil {
		logger.Error(err)
		return "", err
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	logger.Info("handlePubOffer end, will return answer sdp:\n")
	logger.Info(peerConnection.LocalDescription())

	// Get the LocalDescription and take it to base64 so we can paste in browser
	return encode(peerConnection.LocalDescription()), nil
}

// JSON encode + base64 a SessionDescription

func encode(obj *webrtc.SessionDescription) string {
	b, err := json.Marshal(obj)
	if err != nil {
		panic(err)
	}

	return base64.StdEncoding.EncodeToString(b)
}

// Decode a base64 and unmarshal JSON into a SessionDescription
func decode(in string, obj *webrtc.SessionDescription) error {
	b, err := base64.StdEncoding.DecodeString(in)
	if err != nil {
		logger.Error(err)
		return err
	}

	if err = json.Unmarshal(b, obj); err != nil {
		logger.Error(err)
		return err
	}
	return nil
}

func CreateConfRoom(name string) (*ConfRoom, error) {
	logger.Info("CreateConfRoom comming...")
	newRoom := &ConfRoom{
		Name:               name,
		SubLocalVideoTrack: make(map[string]*webrtc.TrackLocalStaticRTP, 0),
		SublocalAudioTrack: make(map[string]*webrtc.TrackLocalStaticRTP, 0),
		CreatedAt:          time.Now(), // 记录创建时间
		PubLocalAudioChan:  make(chan *rtp.Packet),
		PubQuit:            false,
		IsPlayingFile:      false,
	}

	ConfRoomList[name] = newRoom
	logger.Info("CreateConfRoom end")
	return newRoom, nil
}

func PubLocalAudioWrite(rtpChan chan *rtp.Packet, confRoom *ConfRoom, payloadType uint8) {
	var localSeqNum uint16 = 0
	logger.Info("PubLocalAudioWrite loop...")
	defer logger.Info("PubLocalAudioWrite loop end.")
	for rtpPackage := range rtpChan {
		rtpPackage.Header.SequenceNumber = localSeqNum
		rtpPackage.Header.PayloadType = payloadType
		localSeqNum++
		if localSeqNum == 0 {
			localSeqNum = 1 // 避免序列号溢出后变为0
		}
		// logger.Info(rtpPackage)
		err := confRoom.PubLocalAudioTrack.WriteRTP(rtpPackage)
		if err != nil && !errors.Is(err, io.ErrClosedPipe) {
			logger.Errorf("Sub audio write error: %v", err)
			break
		}
	}

}
