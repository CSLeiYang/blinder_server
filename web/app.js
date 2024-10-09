document.getElementById('mute-btn').addEventListener('click', toggleMute);
document.getElementById('confInfoBtn').addEventListener('click', getConfInfo);

// 添加控制杆按钮的事件监听器
document.getElementById('up-btn').addEventListener('click', () => sendControlCommand('forward'));
document.getElementById('down-btn').addEventListener('click', () => sendControlCommand('backward'));
document.getElementById('left-btn').addEventListener('click', () => sendControlCommand('left'));
document.getElementById('right-btn').addEventListener('click', () => sendControlCommand('right'));
document.getElementById('up-left-btn').addEventListener('click', () => sendControlCommand('forwardLeft'));
document.getElementById('up-right-btn').addEventListener('click', () => sendControlCommand('forwardRight'));
document.getElementById('down-left-btn').addEventListener('click', () => sendControlCommand('backwardLeft'));
document.getElementById('down-right-btn').addEventListener('click', () => sendControlCommand('backwardRight'));

// 添加紧急停止按钮的事件监听器
document.getElementById('emergency-stop-btn').addEventListener('click', () => sendControlCommand('emergencyStop'));


let localStream;
let peerConnection;
let isMuted = false;
let isVideoStopped = false;
let confName;
let ws;
async function joinSession(confName) {
    document.getElementById('join-screen').style.display = 'none';
    document.getElementById('participant-view').style.display = 'block';

    peerConnection = new RTCPeerConnection({
        iceServers: [{ urls: 'stun:stun.l.google.com:19302' }]
    });

    localStream = await navigator.mediaDevices.getUserMedia({ video: true, audio: true });
    localStream.getTracks().forEach(track => peerConnection.addTrack(track, localStream));

    ws = new WebSocket(`wss://${window.location.host}/ws`);
    ws.onopen = async () => {
        console.log('Connected to the signaling server');

        const offer = await peerConnection.createOffer();
        await peerConnection.setLocalDescription(offer);
        console.log(JSON.stringify(offer));

        ws.send(JSON.stringify({
            userId: '123456',
            sdp: btoa(JSON.stringify(offer)),
            cmd: 'join',
            roomName: confName
        }));
    };

    let iceCandidates = [];
    peerConnection.onicecandidate = (event) => {
        if (event.candidate) {
            iceCandidates.push(event.candidate);
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
        document.getElementById('remoteVideos').appendChild(el)

        if (event.track.kind === 'video') {
            localStream.getVideoTracks().forEach(track => track.enabled = false);
        }

    };

    // Show video
    // const localVideo = document.createElement('video');
    // localVideo.srcObject = localStream;
    // localVideo.autoplay = true;
    // localVideo.muted = true;
    // document.getElementById('videos').appendChild(localVideo);

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

async function getConfInfo() {
    try {
        const response = await fetch('https://blinder.aiiyou.cn:9443/api/confInfo');
        if (!response.ok) {
            throw new Error('Network response was not ok ' + response.statusText);
        }
        const data = await response.json(); // 假设服务器返回 JSON 格式数据
        const linkContainer = document.getElementById('confInfoResult');
        linkContainer.innerHTML = ''; // 清空之前的内容

        data ? data.forEach(room => {
            // 创建超链接
            const link = document.createElement('a');
            link.href = '#'; // 设置为您希望的链接地址
            link.textContent = room.name; // 使用房间名称作为链接文本
            link.target = '_blank';


            // 添加点击事件
            link.onclick = (e) => {
                e.preventDefault(); // 防止默认行为
                confName = room.name
                joinSession(room.name); // 调用 joinRoom 函数
            };

            // 创建房间创建时间的元素
            const creationTime = document.createElement('span');
            creationTime.textContent = ` (创建时间: ${new Date(room.createdAt).toLocaleString()})`; // 格式化时间

            // 将链接和创建时间添加到容器
            linkContainer.appendChild(link);
            linkContainer.appendChild(creationTime);
            linkContainer.appendChild(document.createElement('br')); // 换行
        }) : linkContainer.innerHTML = 'No active confRoom';

    } catch (error) {
        console.error('There was a problem with the fetch operation:', error);
    }
}


function toggleMute() {
    localStream.getAudioTracks().forEach(track => track.enabled = !track.enabled);
    isMuted = !isMuted;
    document.getElementById('mute-btn').textContent = isMuted ? 'Unmute' : 'Mute';
}

// 发送控制命令的函数
function sendControlCommand(command) {
    if (ws && ws.readyState === WebSocket.OPEN) {
        const message = JSON.stringify({
            cmd: 'control',
            cmdDetail: command,
            userId: '123456', // 你可以根据实际情况替换为实际的用户ID
            roomName: confName
        });
        ws.send(message);
        console.log(`Sent control command: ${command}`);
    } else {
        displayMessage('WebSocket is not open, cannot send control command.', true);
    }
}


getConfInfo()

