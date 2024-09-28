package main

import (
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"time"
	"yanglei_blinder/logger"

	"github.com/gorilla/websocket"
	"github.com/pion/interceptor"
	"github.com/pion/interceptor/pkg/intervalpli"
	"github.com/pion/webrtc/v4"
)


type ConfRoom struct {
	Name                   string
	PubPC                  *webrtc.PeerConnection
	PubRemoteVideoTrack    *webrtc.TrackRemote
	PubRemoteAudioTrack    *webrtc.TrackRemote
	PubLocalAudioTrack     *webrtc.TrackLocalStaticRTP
	SubLocalVideoTrackList []*webrtc.TrackLocalStaticRTP
	SublocalAudioTrackList []*webrtc.TrackLocalStaticRTP
	CreatedAt              time.Time
}

var ConfRoomList = make(map[string]*ConfRoom, 0)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}
var webPort = ":9000"
var httpsPort = ":9443"

func main() {
	http.HandleFunc("/ws", HandleWebSocket)
	fs := http.FileServer(http.Dir("./web"))
	http.Handle("/", fs)

	// 启动 HTTP 服务器
	go func() {
		log.Printf("Starting HTTP server at %s\n", webPort)
		log.Fatal(http.ListenAndServe(webPort, nil))
	}()

	// 启动 HTTPS 服务器
	go func() {
		log.Printf("Starting HTTPS server at %s\n", httpsPort)
		certFile := "./blinder.aiiyou.cn/fullchain.pem" // 替换为你的证书路径
		keyFile := "./blinder.aiiyou.cn/privkey.pem"   // 替换为你的私钥路径
		tlsConfig := &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
		srv := &http.Server{
			Addr:      httpsPort,
			Handler:   nil,
			TLSConfig: tlsConfig,
		}
		log.Fatal(srv.ListenAndServeTLS(certFile, keyFile))
	}()

	select {} // 阻止主 goroutine 退出
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

			answerSdp, err := HandleSubOffer(msg["sdp"].(string), joinRoom)
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

func HandleSubOffer(offer string, confRoom *ConfRoom) (string, error) {
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

		}
	})


	peerConnection.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		if is ==  webrtc.ICEConnectionStateDisconnected || is ==  webrtc.ICEConnectionStateFailed{
			logger.Warn("peerConnection will be close")
			peerConnection.Close()
		}
	})

	//Video track
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

	confRoom.SubLocalVideoTrackList = append(confRoom.SubLocalVideoTrackList, localVideoTrack)
	confRoom.SublocalAudioTrackList = append(confRoom.SublocalAudioTrackList, localAudioTrack)

	logger.Info("handleOffer sub comming end")
	// Get the LocalDescription and take it to base64 so we can paste in browser
	return encode(peerConnection.LocalDescription()), nil

}

func HandlePubOffer(offer string, confRoom *ConfRoom) (string, error) {

	logger.Info("handleOffer comming...")

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

	peerConnection.OnTrack(func(remoteTrack *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) { //nolint: revive
		logger.Info("OnTrack comming....", remoteTrack)
		if remoteTrack.Kind() == webrtc.RTPCodecTypeAudio {
			logger.Info("this is auido track")
			confRoom.PubRemoteAudioTrack = remoteTrack
			rtpBuf := make([]byte, 1400)
			for {
				i, _, readErr := remoteTrack.Read(rtpBuf)
				if readErr != nil {
					logger.Error(readErr)
					return
				}

				for _, localTrack := range confRoom.SublocalAudioTrackList {
					if _, err = localTrack.Write(rtpBuf[:i]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
						logger.Error(err)
						break
					}

				}

			}

		}
		if remoteTrack.Kind() == webrtc.RTPCodecTypeVideo {
			logger.Info("this is video track")
			confRoom.PubRemoteVideoTrack = remoteTrack
			rtpBuf := make([]byte, 1400)
			for {
				i, _, readErr := remoteTrack.Read(rtpBuf)
				if readErr != nil {
					logger.Error(readErr)
					return
				}

				for _, localTrack := range confRoom.SubLocalVideoTrackList {
					if _, err = localTrack.Write(rtpBuf[:i]); err != nil && !errors.Is(err, io.ErrClosedPipe) {
						logger.Error(err)
						break
					}

				}

			}
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

	logger.Info("handleOffer comming end")

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
		Name:      name,
		CreatedAt: time.Now(), // 记录创建时间
	}

	ConfRoomList[name] = newRoom
	logger.Info("CreateConfRoom end")

	return newRoom, nil
}
