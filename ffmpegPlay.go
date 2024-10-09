package main

import (
	"bytes"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"time"
	"yanglei_blinder/logger"

	"github.com/pion/rtp"
)

// EncodeOpusFileToRTPPackets 通过UDP接收FFmpeg生成的RTP数据包，并通过给定的通道返回RTP包。
func FFmpegFileToRTPPackets(filePath string, confRoom *ConfRoom) (int, error) {
	logger.Info("EncodeOpusFileToRTPPackets comming...")
	defer logger.Info("EncodeOpusFileToRTPPackets end")

	confRoom.IsPlayingFile = true
	defer func() {
		confRoom.IsPlayingFile = false
	}()

	// 创建一个UDP监听器以获取随机端口
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		logger.Error(err)
		return 0, err
	}
	defer listener.Close()

	// 获取随机分配的端口号
	udpAddr := listener.LocalAddr().(*net.UDPAddr)
	udpPort := udpAddr.Port

	// 启动FFmpeg进程
	cmd := exec.Command(
		"ffmpeg",
		"-re",
		"-i", filePath, // 输入文件
		"-f", "rtp", // 使用rtp_opus格式
		"-payload_type", "111", // Opus通常使用的payload类型
		"-acodec", "libopus", // 使用Opus编解码器
		"-ar", "48000", // 采样率
		"-ac", "2", // 声道数
		"-b:a", "64k", // 设置比特率为128 kbps
		"-frame_duration", "20",
		"-max_muxing_queue_size", "1024", // 设置最大复用队列大小
		"rtp://127.0.0.1:"+strconv.Itoa(udpPort), // 输出到本地UDP端口
	)

	logger.Info(udpPort)
	cmd.Stderr = &bytes.Buffer{} // 捕获错误日志
	err = cmd.Start()
	if err != nil {
		logger.Error(err)
		return 0, err
	}

	var wg sync.WaitGroup
	wg.Add(1)

	const readTimeout = 800 * time.Millisecond
	// 在一个goroutine中读取UDP数据
	go func() {
		defer wg.Done()
		buf := make([]byte, 1500) // RTP包通常小于1500字节

		for {
			if confRoom.PubQuit || confRoom.PubLocalAudioChan == nil {
				return
			}
			// 设置读取操作的截止时间
			if err := listener.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
				logger.Errorf("Error setting read deadline: %v", err)
				break
			}

			n, _, err := listener.ReadFrom(buf)
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					// 如果是超时错误，可以选择继续循环或者退出
					logger.Error("reading timeout")
					break // 或者 break 来终止循环
				}
				logger.Errorf("Error reading from UDP: %v", err)
				break
			}

			packet := &rtp.Packet{}
			if err := packet.Unmarshal(buf[:n]); err != nil {
				logger.Errorf("Error unmarshalling RTP packet: %v", err)
				continue
			}

			confRoom.PubLocalAudioChan <- packet
		}
	}()

	// 等待FFmpeg进程结束
	err = cmd.Wait()
	if err != nil {
		logger.Errorf("FFmpeg exited with error: %v", err)
		return udpPort, err
	}
	wg.Wait()

	return udpPort, nil
}
