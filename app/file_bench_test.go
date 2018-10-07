// Copyright (c) 2018-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package app

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"io/ioutil"
	"testing"
	"time"

	"github.com/disintegration/imaging"

	"github.com/mattermost/mattermost-server/mlog"
	"github.com/mattermost/mattermost-server/model"
)

func BenchmarkUploadFile(b *testing.B) {
	prepareTestImages(b)
	th := Setup().InitBasic()
	defer th.TearDown()
	// disable logging in the benchmark, as best we can
	th.App.Log.SetConsoleLevel(mlog.LevelError)
	teamId := model.NewId()
	channelId := model.NewId()
	userId := model.NewId()

	mb := func(i int) int {
		return (i + 512*1024) / (1024 * 1024)
	}

	files := []struct {
		title string
		ext   string
		data  []byte
	}{
		{fmt.Sprintf("random-%dMb-gif", mb(len(randomGIF))), ".gif", randomGIF},
		{fmt.Sprintf("random-%dMb-jpg", mb(len(randomJPEG))), ".jpg", randomJPEG},
	}

	file_benchmarks := []struct {
		title string
		f     func(b *testing.B, n int, data []byte, ext string)
	}{
		{
			title: "prepareImage",
			f: func(b *testing.B, n int, data []byte, ext string) {
				_, _, _ = prepareImage(data)
			},
		},
		{
			title: "DoUploadFile",
			f: func(b *testing.B, n int, data []byte, ext string) {
				info1, err := th.App.DoUploadFile(time.Now(), teamId, channelId,
					userId, fmt.Sprintf("BenchmarkDoUploadFile-%d%s", n, ext), data)
				if err != nil {
					b.Fatal(err)
				} else {
					defer func() {
						<-th.App.Srv.Store.FileInfo().PermanentDelete(info1.Id)
						th.App.RemoveFile(info1.Path)
					}()
				}

			},
		},
		{
			title: "UploadFiles",
			f: func(b *testing.B, n int, data []byte, ext string) {
				resp, err := th.App.UploadFiles(teamId, channelId, userId,
					[]io.ReadCloser{ioutil.NopCloser(bytes.NewReader(data))},
					[]string{fmt.Sprintf("BenchmarkDoUploadFiles-%d%s", n, ext)},
					[]string{},
					time.Now())
				if err != nil {
					b.Fatal(err)
				} else {
					defer func() {
						<-th.App.Srv.Store.FileInfo().PermanentDelete(resp.FileInfos[0].Id)
						th.App.RemoveFile(resp.FileInfos[0].Path)
					}()
				}

			},
		},
		{
			title: "UploadFile",
			f: func(b *testing.B, n int, data []byte, ext string) {
				info, err := th.App.UploadFile(&UploadFileContext{
					Timestamp:     time.Now(),
					TeamId:        teamId,
					ChannelId:     channelId,
					UserId:        userId,
					Name:          fmt.Sprintf("BenchmarkDoUploadFile-%d%s", n, ext),
					ContentLength: -1,
					Input:         bytes.NewReader(data),
				})
				if err != nil {
					b.Fatal(err)
				} else {
					defer func() {
						<-th.App.Srv.Store.FileInfo().PermanentDelete(info.Id)
						th.App.RemoveFile(info.Path)
					}()
				}

			},
		},
	}

	for _, fb := range file_benchmarks {
		for _, file := range files {
			b.Run(fb.title+"-"+file.title, func(b *testing.B) {
				for i := 0; i < b.N; i++ {
					fb.f(b, i, file.data, file.ext)
				}
			})
		}
	}
}

func BenchmarkUploadImageProcessing(b *testing.B) {
	prepareTestImages(b)

	image_benchmarks := []struct {
		title string
		f     func(b *testing.B, img image.Image)
	}{
		{
			title: "(thumbnail)Resize",
			f: func(b *testing.B, img image.Image) {
				_ = imaging.Resize(img, 0, IMAGE_THUMBNAIL_PIXEL_HEIGHT, imaging.Lanczos)
			},
		},
		{
			title: "(preview)Resize",
			f: func(b *testing.B, img image.Image) {
				_ = imaging.Resize(img, IMAGE_PREVIEW_PIXEL_WIDTH, 0, imaging.Lanczos)
			},
		},
	}

	for _, ib := range image_benchmarks {
		b.Run(ib.title, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				ib.f(b, rgba)
			}
		})
	}
}
