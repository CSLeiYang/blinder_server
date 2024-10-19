document.getElementById('mute-btn').addEventListener('click', toggleMute);
document.getElementById('confInfoBtn').addEventListener('click', getConfInfo);

// 添加控制杆按钮的事件监听器
document.getElementById('up-btn').addEventListener('click', () => sendControlCommand('zhi_xing'));
document.getElementById('down-btn').addEventListener('click', () => sendControlCommand('hou_zhuan'));
document.getElementById('left-btn').addEventListener('click', () => sendControlCommand('zuo_zhuan'));
document.getElementById('right-btn').addEventListener('click', () => sendControlCommand('you_zhuan'));
document.getElementById('up-left-btn').addEventListener('click', () => sendControlCommand('zuo_yi_dian'));
document.getElementById('up-right-btn').addEventListener('click', () => sendControlCommand('you_yi_dian'));
document.getElementById('down-left-btn').addEventListener('click', () => sendControlCommand('zuo_hou'));
document.getElementById('down-right-btn').addEventListener('click', () => sendControlCommand('you_hou'));

// 添加紧急停止按钮的事件监听器
document.getElementById('emergency-stop-btn').addEventListener('click', () => sendControlCommand('ting'));


// 监听键盘事件以支持快捷键
document.addEventListener('keydown', (event) => {
    switch (event.key) {
        case 'ArrowUp':
        case 'w':
            sendControlCommand('zhi_xing');
            break;
        case 'ArrowDown':
        case 's':
            sendControlCommand('hou_zhuan');
            break;
        case 'ArrowLeft':
        case 'a':
            sendControlCommand('zuo_zhuan');
            break;
        case 'ArrowRight':
        case 'd':
            sendControlCommand('you_zhuan');
            break;
        case 'q': // 假设 q 键代表左上方向
            sendControlCommand('zuo_yi_dian');
            break;
        case 'e': // 假设 e 键代表右上方向
            sendControlCommand('you_yi_dian');
            break;
        case 'z': // 假设 z 键代表左下方向
            sendControlCommand('zuo_hou');
            break;
        case 'c': // 假设 c 键代表右下方向
            sendControlCommand('you_hou');
            break;
        case ' ': // 空格键作为紧急停止
            sendControlCommand('ting');
            break;
    }
});


let localStream;
let peerConnection;
let isMuted = false;
let isVideoStopped = false;
let confName;
let ws;
async function joinSession(confName) {
    document.getElementById('join-screen').style.display = 'none';
    document.getElementById('participant-view').style.display = 'flex';

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
        const response = await fetch(`https://${window.location.host}/api/confInfo`);
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
        console.log('WebSocket is not open, cannot send control command.', true);
    }
}


getConfInfo()


/*自动导航*/
let localWs; // 用于连接本地 WebSocket
let frameInterval; // 用于存储 setInterval 的引用
let isConnected = false; // 用于跟踪 WebSocket 连接状态
const localWsUrl = "ws://127.0.0.1:19999"

// Function to display event messages and log them to the console
function displayEventMessage(message) {
    const eventLog = document.getElementById('event-log');
    const messageElement = document.createElement('div');
    const timestamp = new Date().toLocaleTimeString();

    // Format message
    const formattedMessage = `${timestamp}: ${message}`;
    messageElement.textContent = formattedMessage;
    eventLog.appendChild(messageElement);

    // Scroll to the bottom of the event log
    eventLog.scrollTop = eventLog.scrollHeight;

    // Log to console
    console.log(formattedMessage);
}

// Updated WebSocket event handling
document.getElementById('connect-local-ws-btn').addEventListener('click', async () => {
    if (isConnected) {
        // 如果已经连接，则断开连接
        displayEventMessage('Disconnecting from local WebSocket server...');
        clearInterval(frameInterval); // 停止发送帧
        localWs.close(); // 关闭 WebSocket
        isConnected = false; // 更新连接状态
        return;
    }

    try {
        localWs = new WebSocket(localWsUrl); // 替换 your_port_here 为实际端口

        localWs.onopen = () => {
            displayEventMessage(`Connected to ${localWsUrl}`);
            isConnected = true; // 更新连接状态
            frameInterval = setInterval(sendVideoFrame, 33); // 每 30 毫秒发送一次帧
        };

        localWs.onmessage = (event) => {
            displayEventMessage(`Received from local WS: ${event.data}`);
            autoNavData = JSON.parse(event.data);

            switch (autoNavData['Direction']) {
                case "l":
                    sendControlCommand("zuo");
                    break;
                case "r":
                    sendControlCommand("you");
                    break;
                case "g":
                    sendControlCommand("zhixing");
                    break;
                case "m":
                    sendControlCommand("man");
                    break;
                default:
                    break;
            }

            if (autoNavData["labelCount"]) {
                const labelCount = autoNavData["labelCount"];
                document.getElementById('labelCount').textContent = labelCount;

                if (labelCount > 5) {
                    document.getElementById("labelCount").style.color = "red";
                } else {
                    document.getElementById("labelCount").style.color = "black"; // Or any default color
                }
            } else {
                document.getElementById('labelCount').textContent = "";
            }
        };

        localWs.onerror = (error) => {
            displayEventMessage(`WebSocket Error:${error.message}`);
        };

        localWs.onclose = () => {
            displayEventMessage('WebSocket connection closed');
            clearInterval(frameInterval); // 清除定时器
            isConnected = false; // 更新连接状态
        };
    } catch (error) {
        displayEventMessage(`Failed to connect to ${localWsUrl}, err: ${error.message} `);
    }
});

async function sendVideoFrame() {
    if (peerConnection && localWs && localWs.readyState === WebSocket.OPEN) {
        const remoteVideo = document.querySelector('#remoteVideos video'); // 根据实际情况获取远端视频元素
        if (!remoteVideo) return; // 如果没有视频元素，直接返回

        const canvas = document.createElement('canvas');
        canvas.width = remoteVideo.videoWidth;
        canvas.height = remoteVideo.videoHeight;

        const context = canvas.getContext('2d');
        context.drawImage(remoteVideo, 0, 0, canvas.width, canvas.height);
        const frameData = canvas.toDataURL('image/jpeg'); // 以 JPEG 格式获取视频帧

        localWs.send(JSON.stringify({ "frame": frameData })); // 发送视频帧数据
        console.log('Sent video frame to local WS');
    }
}

/*自动导航 end*/

