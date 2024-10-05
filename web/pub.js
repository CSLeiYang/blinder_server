document.getElementById('join-btn').addEventListener('click', joinSession);
document.getElementById('mute-btn').addEventListener('click', toggleMute);
document.getElementById('video-btn').addEventListener('click', toggleVideo);
document.getElementById('output-btn').addEventListener('click', toggleAudioOutput); // 绑定切换按钮

let localStream;
let peerConnection;
let isMuted = false;
let isVideoStopped = false;
let wakeLock = null;
let audioOutput = 'default'; // 用于存储当前音频输出设备的 ID
const audioContext = new (window.AudioContext || window.webkitAudioContext)();
let mediaStreamDestination = audioContext.createMediaStreamDestination(); // 创建目标音频流

// 创建一个用于显示错误信息的元素
const errorDisplay = document.getElementById('error-display');

async function joinSession(confName) {
    const name = document.getElementById('name').value;
    if (!name) {
        showError('Please enter your name');
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
                // frameRate: { ideal: 15, max: 30 },
            },
            audio: {
                channelCount: 1,
                maxBitrate: 16000,
            }
        });

    } catch (error) {
        showError(`initLocalStream error: ${error.message}`);
        return;
    }

    // 将音频流连接到目标音频流
    localStream.getAudioTracks().forEach(track => {
        const source = audioContext.createMediaStreamSource(localStream);
        source.connect(mediaStreamDestination);
    });

    // 添加目标音频流到对等连接
    peerConnection.addTrack(mediaStreamDestination.stream.getAudioTracks()[0], mediaStreamDestination.stream);

    localStream.getTracks().forEach(track => {
        peerConnection.addTrack(track, localStream);
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

    peerConnection.onicecandidate = (event) => {
        if (event.candidate) {
            // handle candidate
        }
    };

    peerConnection.oniceconnectionstatechange = async () => {
        console.log(`ICE Connection State: ${peerConnection.iceConnectionState}`);
        if (peerConnection.iceConnectionState === 'connected') {
            try {
                // 定期触发小的 DOM 更新
                setInterval(() => {
                    document.body.style.backgroundColor = document.body.style.backgroundColor === 'lightblue' ? 'lightgreen' : 'lightblue';

                }, 10000); // 每10秒更新一次可见性
                wakeLock = await navigator.wakeLock.request('screen');
                console.log('Wake Lock active');

            } catch (err) {
                showError(`${err.name}, ${err.message}`);
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

// 显示错误信息的函数
function showError(message) {
    errorDisplay.textContent = message; // 更新错误信息
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

// 切换音频输出设备
async function toggleAudioOutput() {
    const devices = await navigator.mediaDevices.enumerateDevices();
    const audioOutputs = devices.filter(device => device.kind === 'audiooutput');

    // 获取当前音频输出设备
    audioOutputDeviceId = audioOutputDeviceId === null || audioOutputDeviceId === audioOutputs[0].deviceId
        ? audioOutputs[1]?.deviceId // 切换到下一个设备
        : audioOutputs[0]?.deviceId; // 否则切换回第一个设备

    // 如果有可用设备，设置音频输出
    if (audioOutputDeviceId) {
        const localVideo = document.getElementById('local-video');
        localVideo.setSinkId(audioOutputDeviceId)
            .then(() => {
                console.log(`Audio output set to device: ${audioOutputDeviceId}`);
            })
            .catch(error => {
                console.error('Error setting audio output:', error);
                showError(`Audio output error: ${error.message}`);
            });
    }
}