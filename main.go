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
	"strings"
	"time"
	"yanglei_blinder/logger"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
)

type ConfRoom struct {
	Name                string
	PubPC               *webrtc.PeerConnection
	PubRemoteVideoTrack *webrtc.TrackRemote
	PubRemoteAudioTrack *webrtc.TrackRemote
	PubLocalAudioTrack  *webrtc.TrackLocalStaticRTP
	SubLocalVideoTrack  map[string]*webrtc.TrackLocalStaticRTP
	SublocalAudioTrack  map[string]*webrtc.TrackLocalStaticRTP
	CreatedAt           time.Time
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
	http.HandleFunc("/api/conf", HandlePostConf)        // New POST endpoint
	http.HandleFunc("/api/confInfo", HandleGetConfInfo) // 新增的 GET endpoint

	// 启动 HTTP 服务器
	go func() {
		log.Printf("Starting HTTP server at %s\n", webPort)
		log.Fatal(http.ListenAndServe(webPort, nil))
	}()

	// 启动 HTTPS 服务器
	go func() {
		log.Printf("Starting HTTPS server at %s\n", httpsPort)
		certFile := "./blinder.aiiyou.cn/fullchain.pem" // 替换为你的证书路径
		keyFile := "./blinder.aiiyou.cn/privkey.pem"    // 替换为你的私钥路径
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

// HandlePostConf processes HTTP POST requests for creating or joining a conference room.
func HandlePostConf(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var msg map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	msgCmd := msg["cmd"].(string)

	switch msgCmd {
	case "create":
		roomName := msg["roomName"].(string)
		if len(roomName) == 0 {
			http.Error(w, "Invalid room name", http.StatusBadRequest)
			return
		}
		createdRoom, err := CreateConfRoom(roomName)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		answerSdp, err := HandlePubOffer(msg["sdp"].(string), createdRoom)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonData, _ := json.Marshal(map[string]string{"answer": answerSdp, "type": "answer"})
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)

	case "join":
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
		answerSdp, err := HandleSubOffer(msg["userId"].(string), msg["sdp"].(string), joinRoom)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		jsonData, _ := json.Marshal(map[string]string{"answer": answerSdp, "type": "answer"})
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonData)

	default:
		http.Error(w, "Invalid command", http.StatusBadRequest)
	}
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

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			go func() {
				logger.Info("Audio Track")
				rtpBuf := make([]byte, 1400)
				for {
					i, _, readErr := remoteTrack.Read(rtpBuf)
					if readErr != nil {
						logger.Error(readErr)
						return
					}

					if _, err = confRoom.PubLocalAudioTrack.Write(rtpBuf[:i]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
						logger.Error(err)
						break
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

	// localVideoTrack, newTrackErr := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeVP8}, "video", "pion")
	// if newTrackErr != nil {
	// 	logger.Error(newTrackErr)
	// 	return "", newTrackErr
	// }

	// rtpVideoSender, err := peerConnection.AddTrack(localVideoTrack)
	// if err != nil {
	// 	logger.Error(err)
	// 	return "", err
	// }
	// go func() {
	// 	rtcpBuf := make([]byte, 1500)
	// 	for {
	// 		if _, _, rtcpErr := rtpVideoSender.Read(rtcpBuf); rtcpErr != nil {
	// 			return
	// 		}
	// 	}
	// }()

	localAudioTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypePCMU}, "audio", "pion")
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

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
		logger.Info("OnTrack comming....", remoteTrack)
		if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			go func() {
				logger.Info("this is auido track")
				codec := remoteTrack.Codec()
				confRoom.PubRemoteAudioTrack = remoteTrack
				rtpBuf := make([]byte, 1400)
				// 创建或打开音频录制文件
				audioFileName := fmt.Sprintf("%s/%s_pub_audio_%v_%v_%v_%v.raw", recordPath, confRoom.Name, strings.Split(codec.MimeType, "/")[1], codec.Channels, codec.ClockRate, confRoom.CreatedAt.Format("2006-01-02-15_04_05"))
				audioFile, err := os.OpenFile(audioFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
				if err != nil {
					logger.Error(err)
					return
				}
				defer audioFile.Close()

				for {

					i, _, readErr := remoteTrack.Read(rtpBuf)
					if readErr != nil {
						logger.Error(readErr)
						return
					}

					if _, err := audioFile.Write(rtpBuf[:i]); err != nil {
						logger.Error(err)
						return
					}

					for _, localTrack := range confRoom.SublocalAudioTrack {
						wi, err := localTrack.Write(rtpBuf[:i])
						if err != nil && !errors.Is(err, io.ErrClosedPipe) {
							logger.Error(err)
							break
						} else {
							logger.Infof("audio ri: %v wi:%v", i, wi)
						}

					}

				}
			}()
		}
		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			go func() {
				logger.Info("this is video track")
				codec := remoteTrack.Codec()
				confRoom.PubRemoteVideoTrack = remoteTrack
				rtpBuf := make([]byte, 1400)
				// 创建或打开音频录制文件
				videoFileName := fmt.Sprintf("%s/%s_pub_video_%v_%v_%v.raw", recordPath, confRoom.Name, strings.Split(codec.MimeType, "/")[1], codec.ClockRate, confRoom.CreatedAt.Format("2006-01-02-15_04_05"))
				videoFile, err := os.OpenFile(videoFileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
				if err != nil {
					logger.Error(err)
					return
				}
				defer videoFile.Close()
				for {
					i, _, readErr := remoteTrack.Read(rtpBuf)
					if readErr != nil {
						logger.Error(readErr)
						return
					}

					if _, err := videoFile.Write(rtpBuf[:i]); err != nil {
						logger.Error(err)
						return
					}

					for _, localTrack := range confRoom.SubLocalVideoTrack {
						wi, err := localTrack.Write(rtpBuf[:i])
						if err != nil && !errors.Is(err, io.ErrClosedPipe) {
							logger.Error(err)
							break
						} else {
							logger.Infof("video ri:%v wi:%v:", i, wi)
						}

					}

				}

			}()
		}

	})

	peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		if is == webrtc.ICEConnectionStateFailed || is == webrtc.ICEConnectionStateDisconnected || is == webrtc.ICEConnectionStateClosed {
			peerConnection.Close()
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

	// 在这里修改 answer.SDP
	answerSDP := strings.ReplaceAll(answer.SDP, "opus/48000/2", "opus/24000/1") // 添加你的修改函数

	// Set modified answer SD
	err = peerConnection.SetLocalDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  answerSDP,
	})
	if err != nil {
		logger.Error(err)
		return "", err
	}
	// Sets the LocalDescription, and starts our UDP listeners
	// err = peerConnection.SetLocalDescription(answer)
	// if err != nil {
	// 	logger.Error(err)
	// 	return "", err
	// }

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

	}

	ConfRoomList[name] = newRoom

	logger.Info("CreateConfRoom end")

	return newRoom, nil
}
