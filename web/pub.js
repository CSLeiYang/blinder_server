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





// 页面加载时初始化 localStream 并获取支持的分辨率
async function init() {
    try {
        localStream = await navigator.mediaDevices.getUserMedia({
            video: true,
            audio: true
        });

        const videoTrack = localStream.getVideoTracks()[0];
        if (videoTrack) {
            const capabilities = videoTrack.getCapabilities();
            const supportedResolutions = getSupportedResolutions(capabilities);
            populateResolutionOptions(supportedResolutions);

            // 在这里添加分辨率选择的监听器
            addResolutionChangeListeners();
        }

        // 更新本地视频元素
        const localVideo = document.getElementById('local-video');
        if (localVideo) {
            localVideo.srcObject = localStream;
        }

    } catch (error) {
        displayMessage(`initLocalStream error: ${error.message}`, true); // 使用新的函数名并标记为错误
    }
}

const commonResolutions = [
    { width: 120, height: 160 },  // 竖屏 3:4
    { width: 240, height: 320 },  // 竖屏 3:4
    { width: 480, height: 640 },  // 竖屏 3:4
];
function getSupportedResolutions(capabilities) {
    return commonResolutions.filter(resolution => {
        const { width, height } = resolution;
        return (
            (capabilities.width.min <= width && width <= capabilities.width.max) &&
            (capabilities.height.min <= height && height <= capabilities.height.max)
        );
    }).map(resolution => `${resolution.width}x${resolution.height}`);
}
function populateResolutionOptions(resolutions) {
    const resolutionSelection = document.getElementById('resolution-selection');
    resolutionSelection.innerHTML = ''; // 清空现有的选项

    resolutions.forEach(resolution => {
        const [width, height] = resolution.split('x');
        const label = document.createElement('label');
        const input = document.createElement('input');
        input.type = 'radio';
        input.name = 'resolution';
        input.value = resolution;
        input.checked = (width === '480' && height === '640');
        label.appendChild(input);
        label.appendChild(document.createTextNode(` ${width}x${height}`));
        resolutionSelection.appendChild(label);
    });
}

// 在页面加载时调用 init 函数
init();

function addResolutionChangeListeners() {
    document.querySelectorAll('input[name="resolution"]').forEach(radio => {
        radio.addEventListener('change', async () => {
            await updateLocalStream();
        });
    });
}

async function updateLocalStream() {
    const resolution = document.querySelector('input[name="resolution"]:checked').value.split('x');
    const [width, height] = resolution.map(Number);

    try {
        localStream = await navigator.mediaDevices.getUserMedia({
            video: {
                facingMode: { ideal: 'environment' },
                width: { ideal: width },
                height: { ideal: height },
                frameRate: { ideal: 30 },
            },
            audio: {
                channelCount: 1,
                maxBitrate: 16000,
            }
        });

    } catch (error) {
        displayMessage(`initLocalStream error: ${error.message}`, true); // 使用新的函数名并标记为错误
    }
}

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
        let message;
        switch (peerConnection.iceConnectionState) {
            case 'new':
                message = '正在建立连接...';
                break;
            case 'checking':
                message = '检查网络连接...';
                break;
            case 'connected':
                message = '已连接！';
                try {
                    document.body.style.backgroundColor = document.body.style.backgroundColor === 'lightblue' ? 'lightgreen' : 'lightblue';
                    setInterval(() => {
                        document.body.style.backgroundColor = document.body.style.backgroundColor === 'lightblue' ? 'lightgreen' : 'lightblue';
                    }, 10000);
                    wakeLock = await navigator.wakeLock.request('screen');
                    console.log('Wake Lock active');
                } catch (err) {
                    displayMessage(`${err.name}, ${err.message}`, true); // 使用新的函数名并标记为错误
                }
                break;
            case 'completed':
                message = '连接已完成。';
                break;
            case 'failed':
                message = '连接失败，请重试。';
                break;
            case 'disconnected':
                message = '已断开连接。';
                document.body.style.backgroundColor = 'red'
                break;
            case 'closed':
                message = '连接已关闭。';
                break;
            default:
                message = '未知状态。';
                break;
        }
        // 显示消息
        displayMessage(message);
    }

    peerConnection.ontrack = (event) => {
        const el = document.createElement(event.track.kind);
        el.srcObject = event.streams[0];
        el.autoplay = true;
        el.controls = true;
        document.getElementById('remote-videos').appendChild(el);
    };

    const localVideo = document.getElementById('video');
    localVideo.srcObject = localStream;


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

// 修改显示消息的函数，使其更加通用
function displayMessage(message, isError = false) {
    const errorDisplay = document.getElementById('error-display');
    errorDisplay.textContent = message;
    // 可以根据是否是错误来改变样式
    if (isError) {
        errorDisplay.style.color = 'red';
    } else {
        errorDisplay.style.color = 'black'; // 或者其他颜色
    }
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