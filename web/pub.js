document.getElementById('join-btn').addEventListener('click', joinSession);
document.getElementById('mute-btn').addEventListener('click', toggleMute);
document.getElementById('video-btn').addEventListener('click', toggleVideo);

let localStream;
let peerConnection;
let isMuted = false;
let isVideoStopped = false;
let wakeLock = null; // 声明唤醒锁变量

async function joinSession(confName) {
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
                facingMode: { ideal: 'environment' },
                width: { ideal: 640 },
                height: { ideal: 360 },
                frameRate: { ideal: 15, max: 30 },
            },
            audio: {
                channelCount: 1,
                maxBitrate: 16000,
            }
        });

    } catch (error) {
        alert(`initLocalStream error: ${error}`);
        return; // 处理错误后退出
    }

    localStream.getTracks().forEach(track => {
        const sender = peerConnection.addTrack(track, localStream);
        const parameters = sender.getParameters();

        if (track.kind === 'video') {
            if (!parameters.encodings) {
                parameters.encodings = [{}];
            }
            parameters.encodings[0].maxBitrate = 250000;
            sender.setParameters(parameters);
        }

        if (track.kind === 'audio') {
            if (!parameters.encodings) {
                parameters.encodings = [{}];
            }
            parameters.encodings[0].maxBitrate = 16000;
            parameters.encodings[0].channelCount = 1;
            // sender.setParameters(parameters);
        }
    });

    const ws = new WebSocket(`wss://${window.location.host}/ws`);
    ws.onopen = async () => {
        console.log('Connected to the signaling server');

        const offer = await peerConnection.createOffer();
        await peerConnection.setLocalDescription(offer);
        console.log(JSON.stringify(offer));

        ws.send(JSON.stringify({
            userId: '123456',
            sdp: btoa(JSON.stringify(offer)),
            cmd: 'create',
            roomName: name
        }));
    };

    let iceCandidates = [];
    peerConnection.onicecandidate = (event) => {
        if (event.candidate) {
            iceCandidates.push(event.candidate);
        }
    };

    peerConnection.oniceconnectionstatechange = async () => {
        console.log(`ICE Connection State: ${peerConnection.iceConnectionState}`);
        if (peerConnection.iceConnectionState === 'connected') {
            // 请求屏幕唤醒锁
            try {
                wakeLock = await navigator.wakeLock.request('screen');
                console.log('Wake Lock active');
            } catch (err) {
                console.error(`${err.name}, ${err.message}`);
            }
        }
    };

    peerConnection.ontrack = (event) => {
        const el = document.createElement(event.track.kind);
        el.srcObject = event.streams[0];
        el.autoplay = true;
        el.controls = true;
        document.getElementById('remote-videos').appendChild(el);
    };

    const localVideo = document.createElement('video');
    localVideo.id = 'local-video';
    localVideo.srcObject = localStream;
    localVideo.autoplay = true;
    localVideo.muted = true;
    document.getElementById('local-video-container').appendChild(localVideo);

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
