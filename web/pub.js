
document.getElementById('join-btn').addEventListener('click', joinSession);
document.getElementById('mute-btn').addEventListener('click', toggleMute);
document.getElementById('video-btn').addEventListener('click', toggleVideo);


let localStream;
let peerConnection;
let isMuted = false;
let isVideoStopped = false;
let iceCandidates = [];

async function joinSession() {
    const name = document.getElementById('name').value;
    if (!name) {
        alert('Please enter your name');
        return;
    }

    document.getElementById('join-screen').style.display = 'none';
    document.getElementById('participant-view').style.display = 'block';

    peerConnection = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
    });
    try {
        localStream = await navigator.mediaDevices.getUserMedia({
            video: {
                facingMode: { ideal: 'environment' }, // 使用后置摄像头
                width: { ideal: 640 }, // 理想宽度
                height: { ideal: 360 }, // 理想高度
                frameRate: { ideal: 15, max: 30 } // 最大帧率
            },
            audio: true
        });
        localStream.getTracks().forEach(track => peerConnection.addTrack(track, localStream));
    } catch (error) {
        console.error('获取媒体流失败:', error);
        alert('获取媒体流失败: ' + error.message);
    }



    const ws = new WebSocket(`wss://${window.location.host}/ws`);
    ws.onopen = async () => {
        console.log('Connected to the signaling server');

    };



    async function createOffer() {
        try {
            const offer = await peerConnection.createOffer();
            await peerConnection.setLocalDescription(offer);
            console.log("生成的 SDP:", offer.sdp);
            ws.send(JSON.stringify({
                userId: '123456',
                sdp: btoa(JSON.stringify(offer)),
                cmd: 'create',
                roomName: name
            }));
        } catch (error) {
            console.error("创建 offer 时出错:", error);
        }
    }

    peerConnection.onicecandidate = (event) => {
        console.log(`onicecandidate: ${JSON.stringify(event)}`)
        if (event.candidate) {
            iceCandidates.push(event.candidate);
        } else {
            // 所有候选者收集完成
            console.log("所有候选者已收集:", iceCandidates);
            createOffer(); // 调用 createOffer
        }
    };

    peerConnection.oniceconnectionstatechange = () => {
        console.log(`ICE Connection State: ${peerConnection.iceConnectionState}`);
    };

    peerConnection.ontrack = (event) => {
        const el = document.createElement(event.track.kind)
        el.srcObject = event.streams[0]
        el.autoplay = true
        el.controls = true
        document.getElementById('videos').appendChild(el)

    };
    //trigger ice collection
    await peerConnection.createOffer();

    //Show video
    const localVideo = document.createElement('video');
    localVideo.srcObject = localStream;
    localVideo.autoplay = true;
    localVideo.muted = true;
    document.getElementById('videos').appendChild(localVideo);

    ws.onmessage = async (event) => {
        const jsonObject = JSON.parse(event.data);
        switch (jsonObject['type']) {
            case 'answer':
                const answerStr = atob(jsonObject["answer"]);
                const answerObject = JSON.parse(answerStr);
                console.log(`Recv answer sdp:\n${answerStr}`);
                await peerConnection.setRemoteDescription(new RTCSessionDescription(answerObject));
                break;
            default:
                break;
        }
    };


}


function toggleMute() {
    localStream.getAudioTracks().forEach(track => track.enabled = !track.enabled);
    isMuted = !isMuted;
    document.getElementById('mute-btn').textContent = isMuted ? 'Unmute' : 'Mute';
}

function toggleVideo() {
    localStream.getVideoTracks().forEach(track => track.enabled = !track.enabled);
    isVideoStopped = !isVideoStopped;
    document.getElementById('video-btn').textContent = isVideoStopped ? 'Start Video' : 'Stop Video';
}


