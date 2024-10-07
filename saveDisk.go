// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// save-to-webm is a simple application that shows how to receive audio and video using Pion and then save to WebM container.
package main

import (
	"fmt"
	"os"
	"sync"
	"time"
	"yanglei_blinder/logger"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
)

type webmSaver struct {
	filenName                      string
	audioWriter, videoWriter       webm.BlockWriteCloser
	audioBuilder, vp8Builder       *samplebuilder.SampleBuilder
	audioTimestamp, videoTimestamp time.Duration

	width, height int
	mu            sync.Mutex
}

func newWebmSaver(fileName string) *webmSaver {
	return &webmSaver{
		filenName:    fileName,
		audioBuilder: samplebuilder.New(10, &codecs.OpusPacket{}, 48000),
		vp8Builder:   samplebuilder.New(100, &codecs.VP8Packet{}, 90000),
	}
}

func (s *webmSaver) Close() {
	logger.Info("Finalizing webm..")
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.audioWriter != nil {
		if err := s.audioWriter.Close(); err != nil {
			logger.Error(err)
			return
		}
	}
	if s.videoWriter != nil {
		if err := s.videoWriter.Close(); err != nil {
			logger.Error(err)
			return
		}
	}
}

func (s *webmSaver) PushOpus(rtpPacket *rtp.Packet) {
	s.audioBuilder.Push(rtpPacket)
	for {
		sample := s.audioBuilder.Pop()
		if sample == nil {
			return
		}
		if s.audioWriter != nil {
			s.audioTimestamp += sample.Duration
			s.mu.Lock()
			_, err := s.audioWriter.Write(true, int64(s.audioTimestamp/time.Millisecond), sample.Data)
			s.mu.Unlock()
			if err != nil {
				logger.Error(err)
				return
			}
		}
	}
}

func (s *webmSaver) PushVP8(rtpPacket *rtp.Packet) {
	s.vp8Builder.Push(rtpPacket)
	for {
		sample := s.vp8Builder.Pop()
		if sample == nil {
			break
		}
		// Read VP8 header.
		videoKeyframe := (sample.Data[0]&0x1 == 0)
		if videoKeyframe {
			// logger.Info("Received a keyframe (VP8).")
			// Keyframe has frame information.
			raw := uint(sample.Data[6]) | uint(sample.Data[7])<<8 | uint(sample.Data[8])<<16 | uint(sample.Data[9])<<24
			width := int(raw & 0x3FFF)
			height := int((raw >> 16) & 0x3FFF)

			if s.width != width || s.height != height {
				logger.Infof("Resolution change detected: (%dx%d)-> %dx%d", s.width, s.height, width, height)
			}

			if s.videoWriter == nil || s.audioWriter == nil || (s.width != width || s.height != height) {
				s.mu.Lock()
				s.InitWriter(s.filenName, false, width, height)
				s.mu.Unlock()
			}
			s.width = width
			s.height = height

		} else {
			// logger.Info("Received a non-keyframe (VP8).")
		}

		if s.videoWriter != nil {
			s.videoTimestamp += sample.Duration
			s.mu.Lock()
			_, err := s.videoWriter.Write(videoKeyframe, int64(s.videoTimestamp/time.Millisecond), sample.Data)
			s.mu.Unlock()
			if err != nil {
				logger.Error(err)
				return
			}
		}
	}
}

func (s *webmSaver) InitWriter(baseFileName string, isH264 bool, width, height int) {
	// 生成新的文件名，包含分辨率信息
	fileName := fmt.Sprintf("%s_%dx%d.webm", baseFileName, width, height)

	// 仅在未初始化或分辨率已更改时初始化写入器
	if s.videoWriter != nil && s.audioWriter != nil && s.width == width && s.height == height {
		return // 无需重新初始化
	}

	if s.audioWriter != nil {
		// 关闭现有的写入器
		if err := s.audioWriter.Close(); err != nil {
			logger.Error(err)
		}
	}
	if s.videoWriter != nil {
		if err := s.videoWriter.Close(); err != nil {
			logger.Error(err)
		}
	}

	// 打开新的文件以进行写入
	w, err := os.OpenFile(fileName, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		logger.Error(err)
		return
	}

	videoMimeType := "V_VP8"
	if isH264 {
		videoMimeType = "V_MPEG4/ISO/AVC"
	}

	// 初始化 WebM 写入器
	ws, err := webm.NewSimpleBlockWriter(w,
		[]webm.TrackEntry{
			{
				Name:            "Audio",
				TrackNumber:     1,
				TrackUID:        12345,
				CodecID:         "A_OPUS",
				TrackType:       2,
				DefaultDuration: 20000000,
				Audio: &webm.Audio{
					SamplingFrequency: 48000.0,
					Channels:          2,
				},
			}, {
				Name:            "Video",
				TrackNumber:     2,
				TrackUID:        67890,
				CodecID:         videoMimeType,
				TrackType:       1,
				DefaultDuration: 33333333,
				Video: &webm.Video{
					PixelWidth:  uint64(width),
					PixelHeight: uint64(height),
				},
			},
		})
	if err != nil {
		logger.Error(err)
		return
	}
	logger.Infof("WebM saver has started with video width=%d, height=%d\n", width, height)
	s.audioWriter = ws[0]
	s.videoWriter = ws[1]
	// 更新当前分辨率
	s.width = width
	s.height = height
}
