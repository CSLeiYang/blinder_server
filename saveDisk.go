// SPDX-FileCopyrightText: 2023 The Pion community <https://pion.ly>
// SPDX-License-Identifier: MIT

//go:build !js
// +build !js

// save-to-webm is a simple application that shows how to receive audio and video using Pion and then save to WebM container.
package main

import (
	"fmt"
	"os"
	"time"
	"yanglei_blinder/logger"

	"github.com/at-wat/ebml-go/webm"
	"github.com/pion/interceptor/pkg/jitterbuffer"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4/pkg/media/samplebuilder"
)

const (
	naluTypeBitmask = 0b11111
	naluTypeSPS     = 7
)

type webmSaver struct {
	filenName                      string
	audioWriter, videoWriter       webm.BlockWriteCloser
	audioBuilder, vp8Builder       *samplebuilder.SampleBuilder
	audioTimestamp, videoTimestamp time.Duration

	h264JitterBuffer   *jitterbuffer.JitterBuffer
	lastVideoTimestamp uint32
}

func newWebmSaver(fileName string) *webmSaver {
	return &webmSaver{
		filenName:        fileName,
		audioBuilder:     samplebuilder.New(10, &codecs.OpusPacket{}, 48000),
		vp8Builder:       samplebuilder.New(10, &codecs.VP8Packet{}, 90000),
		h264JitterBuffer: jitterbuffer.New(),
	}
}

func (s *webmSaver) Close() {
	fmt.Printf("Finalizing webm...\n")
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
			if _, err := s.audioWriter.Write(true, int64(s.audioTimestamp/time.Millisecond), sample.Data); err != nil {
				logger.Error(err)
				return
			}
		}
	}
}

func (s *webmSaver) PushH264(rtpPacket *rtp.Packet) {
	s.h264JitterBuffer.Push(rtpPacket)

	pkt, err := s.h264JitterBuffer.Peek(true)
	if err != nil {
		return
	}

	pkts := []*rtp.Packet{pkt}
	for {
		pkt, err = s.h264JitterBuffer.PeekAtSequence(pkts[len(pkts)-1].SequenceNumber + 1)
		if err != nil {
			return
		}

		// We have popped a whole frame, lets write it
		if pkts[0].Timestamp != pkt.Timestamp {
			break
		}

		pkts = append(pkts, pkt)
	}

	h264Packet := &codecs.H264Packet{}
	data := []byte{}
	for i := range pkts {
		if _, err = s.h264JitterBuffer.PopAtSequence(pkts[i].SequenceNumber); err != nil {
			logger.Error(err)
			return
		}

		out, err := h264Packet.Unmarshal(pkts[i].Payload)
		if err != nil {
			logger.Error(err)
			return
		}
		data = append(data, out...)
	}

	videoKeyframe := (data[4] & naluTypeBitmask) == naluTypeSPS
	if s.videoWriter == nil && videoKeyframe {
		if s.videoWriter == nil || s.audioWriter == nil {
			s.InitWriter(s.filenName,true, 1280, 720)
		}
	}

	samples := uint32(0)
	if s.lastVideoTimestamp != 0 {
		samples = pkts[0].Timestamp - s.lastVideoTimestamp
	}
	s.lastVideoTimestamp = pkts[0].Timestamp

	if s.videoWriter != nil {
		s.videoTimestamp += time.Duration(float64(samples) / float64(90000) * float64(time.Second))
		if _, err := s.videoWriter.Write(videoKeyframe, int64(s.videoTimestamp/time.Millisecond), data); err != nil {
			logger.Error(err)
			return
		}
	}
}

func (s *webmSaver) PushVP8(rtpPacket *rtp.Packet) {
	s.vp8Builder.Push(rtpPacket)

	for {
		sample := s.vp8Builder.Pop()
		if sample == nil {
			return
		}
		// Read VP8 header.
		videoKeyframe := (sample.Data[0]&0x1 == 0)
		if videoKeyframe {
			// Keyframe has frame information.
			raw := uint(sample.Data[6]) | uint(sample.Data[7])<<8 | uint(sample.Data[8])<<16 | uint(sample.Data[9])<<24
			width := int(raw & 0x3FFF)
			height := int((raw >> 16) & 0x3FFF)

			if s.videoWriter == nil || s.audioWriter == nil {
				s.InitWriter(s.filenName, false, width, height)
			}
		}
		if s.videoWriter != nil {
			s.videoTimestamp += sample.Duration
			if _, err := s.videoWriter.Write(videoKeyframe, int64(s.videoTimestamp/time.Millisecond), sample.Data); err != nil {
				logger.Error(err)
				return
			}
		}
	}
}

func (s *webmSaver) InitWriter(fileName string, isH264 bool, width, height int) {
	w, err := os.OpenFile("test.webm", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		logger.Error(err)
		return
	}

	videoMimeType := "V_VP8"
	if isH264 {
		videoMimeType = "V_MPEG4/ISO/AVC"
	}

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
		panic(err)
	}
	fmt.Printf("WebM saver has started with video width=%d, height=%d\n", width, height)
	s.audioWriter = ws[0]
	s.videoWriter = ws[1]
}
