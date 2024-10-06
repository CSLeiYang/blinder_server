package main

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"os"
	"time"
	"yanglei_blinder/logger"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v3/pkg/media/samplebuilder"
	"golang.org/x/image/vp8"
)

func Snapshot(rtpChan chan *rtp.Packet, filePath string, filePrefix string) {
	// Initialized with 20 maxLate, my samples sometimes 10-15 packets
	sampleBuilder := samplebuilder.New(20, &codecs.VP8Packet{}, 90000)
	decoder := vp8.NewDecoder()

	for {
		select {
		case packet, ok := <-rtpChan:
			if !ok {
				// Channel is closed, exit the loop
				return
			}
			sampleBuilder.Push(packet)

			// Use SampleBuilder to generate full picture from many RTP Packets
			sample := sampleBuilder.Pop()
			if sample == nil {
				continue
			}

			// Read VP8 header.
			videoKeyframe := (sample.Data[0]&0x1 == 0)
			if !videoKeyframe {
				continue
			}

			// Begin VP8-to-image decode: Init->DecodeFrameHeader->DecodeFrame
			decoder.Init(bytes.NewReader(sample.Data), len(sample.Data))

			// Decode header
			if _, err := decoder.DecodeFrameHeader(); err != nil {
				logger.Infof("Error decoding frame header: %v", err)
				continue
			}

			// Decode Frame
			img, err := decoder.DecodeFrame()
			if err != nil {
				logger.Infof("Error decoding frame: %v", err)
				continue
			}

			// Encode to (RGB) jpeg
			buffer := new(bytes.Buffer)
			if err = jpeg.Encode(buffer, img, nil); err != nil {
				logger.Infof("Error encoding JPEG: %v", err)
				continue
			}

			// Create file name with path and prefix
			timestamp := time.Now().Format("20060102150405")
			fileName := fmt.Sprintf("%s/%s_%s.jpg", filePath, filePrefix, timestamp)

			// Write jpeg to a local file
			file, err := os.Create(fileName)
			if err != nil {
				logger.Infof("Error creating file: %v", err)
				continue
			}
			defer file.Close() // Ensure the file is closed after writing

			if _, err = file.Write(buffer.Bytes()); err != nil {
				logger.Infof("Error writing to file: %v", err)
				continue
			}

			logger.Infof("Snapshot saved to %s\n", fileName)
		}
	}
}
