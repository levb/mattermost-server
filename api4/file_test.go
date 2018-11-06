// Copyright (c) 2017-present Mattermost, Inc. All Rights Reserved.
// See License.txt for license information.

package api4

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattermost/mattermost-server/app"
	"github.com/mattermost/mattermost-server/mlog"
	"github.com/mattermost/mattermost-server/model"
	"github.com/mattermost/mattermost-server/store"
	"github.com/mattermost/mattermost-server/utils"
)

func BenchmarkUploadImage(b *testing.B) {
	b.StopTimer()

	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	// disable logging in the benchmark, as best we can
	th.App.Log.SetConsoleLevel(mlog.LevelError)
	Client := th.Client
	channel := th.BasicChannel

	benchOne := func(b *testing.B, data []byte, title string) {
		fileResp, resp := Client.UploadFile(data, channel.Id, title)
		if resp.Error != nil {
			b.Fatal(resp.Error)
		}
		if len(fileResp.FileInfos) != 1 {
			b.Fatal("should've returned a single file infos")
		}
		uploadInfo := fileResp.FileInfos[0]

		b.StopTimer()
		result := <-th.App.Srv.Store.FileInfo().Get(uploadInfo.Id)
		if result.Err != nil {
			b.Fatal(result.Err)
		}
		info := result.Data.(*model.FileInfo)

		if err := th.cleanupTestFile(info); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}

	bench := func(b *testing.B, data []byte, tmpl string) {
		for i := 0; i < b.N; i++ {
			benchOne(b, data, fmt.Sprintf(tmpl, i))
		}
	}

	// Create a random image (pre-seeded for predictability)
	rgba := image.NewRGBA(image.Rectangle{
		image.Point{0, 0},
		image.Point{1024, 1024},
	})
	_, err := rand.New(rand.NewSource(1)).Read(rgba.Pix)
	if err != nil {
		b.Fatal(err)
	}

	// Encode it as JPEG and GIF
	buf := &bytes.Buffer{}
	err = jpeg.Encode(buf, rgba, &jpeg.Options{Quality: 50})
	if err != nil {
		b.Fatal(err)
	}
	randomJPEG := buf.Bytes()

	buf = &bytes.Buffer{}
	err = gif.Encode(buf, rgba, nil)
	if err != nil {
		b.Fatal(err)
	}
	randomGIF := buf.Bytes()

	// Run the benchmarks
	b.StartTimer()

	b.Run(
		fmt.Sprintf("Random GIF %dMb\n", (len(randomGIF)+512*1024)/(1024*1024)),
		func(b *testing.B) {
			bench(b, randomGIF, "test%d.gif")
		})

	b.Run(
		fmt.Sprintf("Random JPEG %dMb\n", (len(randomJPEG)+512*1024)/(1024*1024)),
		func(b *testing.B) {
			bench(b, randomJPEG, "test%d.jpg")
		})
}

func testUploadLocalFile(t *testing.T, client *model.Client4, channelId string, name string,
	useMultipart bool, check func(t *testing.T, resp *model.Response), validatePayload bool) *model.FileInfo {

	fileResp := testUploadLocalFiles(t, client, channelId, []string{name}, useMultipart, check, validatePayload)
	if fileResp == nil || len(fileResp.FileInfos) == 0 {
		return nil
	}
	return fileResp.FileInfos[0]
}

func testUploadLocalFiles(t *testing.T, client *model.Client4, channelId string, names []string, useMultipart bool,
	checkResponse func(t *testing.T, resp *model.Response), validatePayloads bool) *model.FileUploadResponse {

	dir, _ := utils.FindDir("tests")
	paths := []string{}

	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}

	fileResp, resp := client.UploadLocalFiles(channelId, paths, nil, nil, useMultipart)

	if checkResponse != nil {
		checkResponse(t, resp)
	}
	if fileResp == nil {
		return nil
	}
	if len(fileResp.FileInfos) != len(names) {
		t.Errorf("Expected %v FileInfos, got %v", len(names), len(fileResp.FileInfos))
	}
	if !validatePayloads {
		return fileResp
	}

	for i, fi := range fileResp.FileInfos {
		f, _ := os.Open(paths[i])
		expected, _ := ioutil.ReadAll(f)
		f.Close()

		data, resp := client.GetFile(fi.Id)
		CheckNoError(t, resp)

		if bytes.Compare(data, expected) != 0 {
			t.Errorf("Mismatched payload")
		}
	}
	return fileResp
}

func checkCond(tb testing.TB, cond bool, text string) {
	if !cond {
		tb.Error(text)
	}
}

func checkEq(tb testing.TB, v1, v2 interface{}, text string) {
	checkCond(tb, fmt.Sprintf("%+v", v1) == fmt.Sprintf("%+v", v2), text)
}

func checkNeq(tb testing.TB, v1, v2 interface{}, text string) {
	checkCond(tb, fmt.Sprintf("%+v", v1) != fmt.Sprintf("%+v", v2), text)
}

func TestUploadFiles(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	channel := th.BasicChannel
	date := time.Now().Format("20060102")

	// Set MaxFileSize to the size of the biggest file in the test.
	th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.MaxFileSize = 279591 })
	th.App.UpdateConfig(func(cfg *model.Config) { *cfg.ServiceSettings.EnableDeveloper = true })

	tests := []struct {
		title              string
		filenames          []string
		client             *model.Client4
		channelId          string
		setupConfig        func(a *app.App) func(a *app.App)
		checkResponse      func(t *testing.T, resp *model.Response)
		expectSuccess      bool
		expectImage        bool
		expectedCreatorId  string
		expectedPathPrefix string
	}{
		{
			title:              "basic",
			filenames:          []string{"test.png", "testgif.gif", "testplugin.tar.gz", "test-config.json"},
			checkResponse:      CheckNoError,
			expectSuccess:      true,
			expectedCreatorId:  th.BasicUser.Id,
			expectedPathPrefix: fmt.Sprintf("%v/teams/%v/channels/%v/users/", date, FILE_TEAM_ID, channel.Id),
		},
		{
			title:              "basic images",
			filenames:          []string{"test.png", "testgif.gif"},
			checkResponse:      CheckNoError,
			expectImage:        true,
			expectSuccess:      true,
			expectedCreatorId:  th.BasicUser.Id,
			expectedPathPrefix: fmt.Sprintf("%v/teams/%v/channels/%v/users/", date, FILE_TEAM_ID, channel.Id),
		},
		{
			title:         "basic channelId does not exist",
			channelId:     model.NewId(),
			filenames:     []string{"test.png"},
			checkResponse: CheckForbiddenStatus,
		},
		{
			title:         "basic invalid channelId",
			channelId:     "../../junk",
			filenames:     []string{"test.png"},
			checkResponse: CheckBadRequestStatus,
		},
		{
			title:              "admin",
			client:             th.SystemAdminClient,
			filenames:          []string{"test.png"},
			checkResponse:      CheckNoError,
			expectSuccess:      true,
			expectedCreatorId:  th.SystemAdminUser.Id,
			expectedPathPrefix: fmt.Sprintf("%v/teams/%v/channels/%v/users/", date, FILE_TEAM_ID, channel.Id),
		},
		{
			title:         "admin channelId does not exist",
			client:        th.SystemAdminClient,
			channelId:     model.NewId(),
			filenames:     []string{"test.png"},
			checkResponse: CheckForbiddenStatus,
		},
		{
			title:         "admin invalid channelId",
			client:        th.SystemAdminClient,
			channelId:     "../../junk",
			filenames:     []string{"test.png"},
			checkResponse: CheckBadRequestStatus,
		},
		{
			title:         "admin disabled",
			client:        th.SystemAdminClient,
			filenames:     []string{"test.png"},
			checkResponse: CheckNotImplementedStatus,
			setupConfig: func(a *app.App) func(a *app.App) {
				enableFileAttachments := *a.Config().FileSettings.EnableFileAttachments
				a.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.EnableFileAttachments = false })
				return func(a *app.App) {
					a.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.EnableFileAttachments = enableFileAttachments })
				}
			},
		},
		{
			title:         "File too large",
			filenames:     []string{"test.png"},
			checkResponse: CheckRequestEntityTooLargeStatus,
			setupConfig: func(a *app.App) func(a *app.App) {
				maxFileSize := *a.Config().FileSettings.MaxFileSize
				a.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.MaxFileSize = 279590 })
				return func(a *app.App) {
					a.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.MaxFileSize = maxFileSize })
				}
			},
		},
	}

	for _, useMultipart := range []bool{true, false} {
		for _, tc := range tests {
			title := ""
			if useMultipart {
				title = "multipart "
			} else {
				title = "simple "
			}
			if tc.title != "" {
				title += tc.title + " "
			}
			title += fmt.Sprintf("%v", tc.filenames)

			client := th.Client
			if tc.client != nil {
				client = tc.client
			}
			channelId := channel.Id
			if tc.channelId != "" {
				channelId = tc.channelId
			}

			var restoreConfig func(a *app.App)
			if tc.setupConfig != nil {
				restoreConfig = tc.setupConfig(th.App)
			}

			t.Run(title, func(t *testing.T) {
				resp := testUploadLocalFiles(t, client, channelId, tc.filenames, useMultipart, tc.checkResponse, true)
				if !tc.expectSuccess {
					return
				}
				if resp == nil || len(resp.FileInfos) == 0 || len(resp.FileInfos) != len(tc.filenames) {
					t.Fatal("Empty or mismatched actual or expected FileInfos")
				}

				for _, ri := range resp.FileInfos {
					// The returned file info from the upload call will be missing some fields that will be stored in the database
					checkEq(t, ri.CreatorId, tc.expectedCreatorId, "File should be assigned to user")
					checkEq(t, ri.PostId, "", "File shouldn't have a post Id")
					checkEq(t, ri.Path, "", "File path should not be set on returned info")
					checkEq(t, ri.ThumbnailPath, "", "File thumbnail path should not be set on returned info")
					checkEq(t, ri.PreviewPath, "", "File preview path should not be set on returned info")

					var dbInfo *model.FileInfo
					result := <-th.App.Srv.Store.FileInfo().Get(ri.Id)
					if result.Err != nil {
						t.Error(result.Err)
					} else {
						dbInfo = result.Data.(*model.FileInfo)
					}
					checkEq(t, dbInfo.Id, ri.Id, "File id from response should match one stored in database")
					checkEq(t, dbInfo.CreatorId, tc.expectedCreatorId, "F ile should be assigned to user")
					checkEq(t, dbInfo.PostId, "", "File shouldn't have a post")
					checkNeq(t, dbInfo.Path, "", "File path should be set in database")
					_, fname := filepath.Split(dbInfo.Path)
					ext := filepath.Ext(fname)
					name := fname[:len(fname)-len(ext)]
					expectedDir := fmt.Sprintf(tc.expectedPathPrefix+"%s/%s", ri.CreatorId, ri.Id)
					expectedPath := fmt.Sprintf("%s/%s", expectedDir, fname)
					checkEq(t, dbInfo.Path, expectedPath,
						fmt.Sprintf("File %v saved to:%q, expected:%q", dbInfo.Name, dbInfo.Path, expectedPath))
					if tc.expectImage {
						expectedThumbnailPath := fmt.Sprintf("%s/%s_thumb.jpg", expectedDir, name)
						expectedPreviewPath := fmt.Sprintf("%s/%s_preview.jpg", expectedDir, name)
						checkEq(t, dbInfo.ThumbnailPath, expectedThumbnailPath,
							fmt.Sprintf("File %v saved to:%q, expected:%q", dbInfo.Name, dbInfo.ThumbnailPath, expectedThumbnailPath))
						checkEq(t, dbInfo.PreviewPath, expectedPreviewPath,
							fmt.Sprintf("File %v saved to:%q, expected:%q", dbInfo.Name, dbInfo.PreviewPath, expectedPreviewPath))
					}

					th.cleanupTestFile(dbInfo)
				}
			})

			if restoreConfig != nil {
				restoreConfig(th.App)
			}
		}
	}
}

func TestGetFile(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	fileId := ""
	var sent []byte
	var err error
	if sent, err = readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(sent, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	data, resp := Client.GetFile(fileId)
	CheckNoError(t, resp)

	if len(data) == 0 {
		t.Fatal("should not be empty")
	}

	for i := range data {
		if data[i] != sent[i] {
			t.Fatal("received file didn't match sent one")
		}
	}

	_, resp = Client.GetFile("junk")
	CheckBadRequestStatus(t, resp)

	_, resp = Client.GetFile(model.NewId())
	CheckNotFoundStatus(t, resp)

	Client.Logout()
	_, resp = Client.GetFile(fileId)
	CheckUnauthorizedStatus(t, resp)

	_, resp = th.SystemAdminClient.GetFile(fileId)
	CheckNoError(t, resp)
}

func TestGetFileHeaders(t *testing.T) {
	th := Setup().InitBasic()
	defer th.TearDown()

	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	testHeaders := func(data []byte, filename string, expectedContentType string, getInline bool) func(*testing.T) {
		return func(t *testing.T) {
			fileResp, resp := Client.UploadFile(data, channel.Id, filename)
			CheckNoError(t, resp)

			fileId := fileResp.FileInfos[0].Id

			_, resp = Client.GetFile(fileId)
			CheckNoError(t, resp)

			if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, expectedContentType) {
				t.Fatal("returned incorrect Content-Type", contentType)
			}

			if getInline {
				if contentDisposition := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(contentDisposition, "inline") {
					t.Fatal("returned incorrect Content-Disposition", contentDisposition)
				}
			} else {
				if contentDisposition := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(contentDisposition, "attachment") {
					t.Fatal("returned incorrect Content-Disposition", contentDisposition)
				}
			}

			_, resp = Client.DownloadFile(fileId, true)
			CheckNoError(t, resp)

			if contentType := resp.Header.Get("Content-Type"); !strings.HasPrefix(contentType, expectedContentType) {
				t.Fatal("returned incorrect Content-Type", contentType)
			}

			if contentDisposition := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(contentDisposition, "attachment") {
				t.Fatal("returned incorrect Content-Disposition", contentDisposition)
			}
		}
	}

	data := []byte("ABC")

	t.Run("png", testHeaders(data, "test.png", "image/png", true))
	t.Run("gif", testHeaders(data, "test.gif", "image/gif", true))
	t.Run("mp4", testHeaders(data, "test.mp4", "video/mp4", true))
	t.Run("mp3", testHeaders(data, "test.mp3", "audio/mpeg", true))
	t.Run("pdf", testHeaders(data, "test.pdf", "application/pdf", false))
	t.Run("txt", testHeaders(data, "test.txt", "text/plain", false))
	t.Run("html", testHeaders(data, "test.html", "text/plain", false))
	t.Run("js", testHeaders(data, "test.js", "text/plain", false))
	t.Run("go", testHeaders(data, "test.go", "application/octet-stream", false))
	t.Run("zip", testHeaders(data, "test.zip", "application/zip", false))
	// Not every platform can recognize these
	//t.Run("exe", testHeaders(data, "test.exe", "application/x-ms", false))
	t.Run("no extension", testHeaders(data, "test", "application/octet-stream", false))
	t.Run("no extension 2", testHeaders([]byte("<html></html>"), "test", "application/octet-stream", false))
}

func TestGetFileThumbnail(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	fileId := ""
	var sent []byte
	var err error
	if sent, err = readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(sent, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	// Wait a bit for files to ready
	time.Sleep(2 * time.Second)

	data, resp := Client.GetFileThumbnail(fileId)
	CheckNoError(t, resp)

	if len(data) == 0 {
		t.Fatal("should not be empty")
	}

	_, resp = Client.GetFileThumbnail("junk")
	CheckBadRequestStatus(t, resp)

	_, resp = Client.GetFileThumbnail(model.NewId())
	CheckNotFoundStatus(t, resp)

	Client.Logout()
	_, resp = Client.GetFileThumbnail(fileId)
	CheckUnauthorizedStatus(t, resp)

	otherUser := th.CreateUser()
	Client.Login(otherUser.Email, otherUser.Password)
	_, resp = Client.GetFileThumbnail(fileId)
	CheckForbiddenStatus(t, resp)

	Client.Logout()
	_, resp = th.SystemAdminClient.GetFileThumbnail(fileId)
	CheckNoError(t, resp)
}

func TestGetFileLink(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	enablePublicLink := th.App.Config().FileSettings.EnablePublicLink
	publicLinkSalt := *th.App.Config().FileSettings.PublicLinkSalt
	defer func() {
		th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = enablePublicLink })
		th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.PublicLinkSalt = publicLinkSalt })
	}()
	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = true })
	th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.PublicLinkSalt = model.NewId() })

	fileId := ""
	if data, err := readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(data, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	_, resp := Client.GetFileLink(fileId)
	CheckBadRequestStatus(t, resp)

	// Hacky way to assign file to a post (usually would be done by CreatePost call)
	store.Must(th.App.Srv.Store.FileInfo().AttachToPost(fileId, th.BasicPost.Id))

	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = false })
	_, resp = Client.GetFileLink(fileId)
	CheckNotImplementedStatus(t, resp)

	// Wait a bit for files to ready
	time.Sleep(2 * time.Second)

	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = true })
	link, resp := Client.GetFileLink(fileId)
	CheckNoError(t, resp)

	if link == "" {
		t.Fatal("should've received public link")
	}

	_, resp = Client.GetFileLink("junk")
	CheckBadRequestStatus(t, resp)

	_, resp = Client.GetFileLink(model.NewId())
	CheckNotFoundStatus(t, resp)

	Client.Logout()
	_, resp = Client.GetFileLink(fileId)
	CheckUnauthorizedStatus(t, resp)

	otherUser := th.CreateUser()
	Client.Login(otherUser.Email, otherUser.Password)
	_, resp = Client.GetFileLink(fileId)
	CheckForbiddenStatus(t, resp)

	Client.Logout()
	_, resp = th.SystemAdminClient.GetFileLink(fileId)
	CheckNoError(t, resp)

	if result := <-th.App.Srv.Store.FileInfo().Get(fileId); result.Err != nil {
		t.Fatal(result.Err)
	} else {
		th.cleanupTestFile(result.Data.(*model.FileInfo))
	}
}

func TestGetFilePreview(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	fileId := ""
	var sent []byte
	var err error
	if sent, err = readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(sent, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	// Wait a bit for files to ready
	time.Sleep(2 * time.Second)

	data, resp := Client.GetFilePreview(fileId)
	CheckNoError(t, resp)

	if len(data) == 0 {
		t.Fatal("should not be empty")
	}

	_, resp = Client.GetFilePreview("junk")
	CheckBadRequestStatus(t, resp)

	_, resp = Client.GetFilePreview(model.NewId())
	CheckNotFoundStatus(t, resp)

	Client.Logout()
	_, resp = Client.GetFilePreview(fileId)
	CheckUnauthorizedStatus(t, resp)

	otherUser := th.CreateUser()
	Client.Login(otherUser.Email, otherUser.Password)
	_, resp = Client.GetFilePreview(fileId)
	CheckForbiddenStatus(t, resp)

	Client.Logout()
	_, resp = th.SystemAdminClient.GetFilePreview(fileId)
	CheckNoError(t, resp)
}

func TestGetFileInfo(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	user := th.BasicUser
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	fileId := ""
	var sent []byte
	var err error
	if sent, err = readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(sent, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	// Wait a bit for files to ready
	time.Sleep(2 * time.Second)

	info, resp := Client.GetFileInfo(fileId)
	CheckNoError(t, resp)

	if err != nil {
		t.Fatal(err)
	} else if info.Id != fileId {
		t.Fatal("got incorrect file")
	} else if info.CreatorId != user.Id {
		t.Fatal("file should be assigned to user")
	} else if info.PostId != "" {
		t.Fatal("file shouldn't have a post")
	} else if info.Path != "" {
		t.Fatal("file path shouldn't have been returned to client")
	} else if info.ThumbnailPath != "" {
		t.Fatal("file thumbnail path shouldn't have been returned to client")
	} else if info.PreviewPath != "" {
		t.Fatal("file preview path shouldn't have been returned to client")
	} else if info.MimeType != "image/png" {
		t.Fatal("mime type should've been image/png")
	}

	_, resp = Client.GetFileInfo("junk")
	CheckBadRequestStatus(t, resp)

	_, resp = Client.GetFileInfo(model.NewId())
	CheckNotFoundStatus(t, resp)

	Client.Logout()
	_, resp = Client.GetFileInfo(fileId)
	CheckUnauthorizedStatus(t, resp)

	otherUser := th.CreateUser()
	Client.Login(otherUser.Email, otherUser.Password)
	_, resp = Client.GetFileInfo(fileId)
	CheckForbiddenStatus(t, resp)

	Client.Logout()
	_, resp = th.SystemAdminClient.GetFileInfo(fileId)
	CheckNoError(t, resp)
}

func TestGetPublicFile(t *testing.T) {
	th := Setup().InitBasic().InitSystemAdmin()
	defer th.TearDown()
	Client := th.Client
	channel := th.BasicChannel

	if *th.App.Config().FileSettings.DriverName == "" {
		t.Skip("skipping because no file driver is enabled")
	}

	enablePublicLink := th.App.Config().FileSettings.EnablePublicLink
	publicLinkSalt := *th.App.Config().FileSettings.PublicLinkSalt
	defer func() {
		th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = enablePublicLink })
		th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.PublicLinkSalt = publicLinkSalt })
	}()
	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = true })
	th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.PublicLinkSalt = GenerateTestId() })

	fileId := ""
	if data, err := readTestFile("test.png"); err != nil {
		t.Fatal(err)
	} else {
		fileResp, resp := Client.UploadFile(data, channel.Id, "test.png")
		CheckNoError(t, resp)

		fileId = fileResp.FileInfos[0].Id
	}

	// Hacky way to assign file to a post (usually would be done by CreatePost call)
	store.Must(th.App.Srv.Store.FileInfo().AttachToPost(fileId, th.BasicPost.Id))

	result := <-th.App.Srv.Store.FileInfo().Get(fileId)
	info := result.Data.(*model.FileInfo)
	link := th.App.GeneratePublicLink(Client.Url, info)

	// Wait a bit for files to ready
	time.Sleep(2 * time.Second)

	if resp, err := http.Get(link); err != nil || resp.StatusCode != http.StatusOK {
		t.Log(link)
		t.Fatal("failed to get image with public link", err)
	}

	if resp, err := http.Get(link[:strings.LastIndex(link, "?")]); err == nil && resp.StatusCode != http.StatusBadRequest {
		t.Fatal("should've failed to get image with public link without hash", resp.Status)
	}

	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = false })
	if resp, err := http.Get(link); err == nil && resp.StatusCode != http.StatusNotImplemented {
		t.Fatal("should've failed to get image with disabled public link")
	}

	// test after the salt has changed
	th.App.UpdateConfig(func(cfg *model.Config) { cfg.FileSettings.EnablePublicLink = true })
	th.App.UpdateConfig(func(cfg *model.Config) { *cfg.FileSettings.PublicLinkSalt = GenerateTestId() })

	if resp, err := http.Get(link); err == nil && resp.StatusCode != http.StatusBadRequest {
		t.Fatal("should've failed to get image with public link after salt changed")
	}

	if resp, err := http.Get(link); err == nil && resp.StatusCode != http.StatusBadRequest {
		t.Fatal("should've failed to get image with public link after salt changed")
	}

	if err := th.cleanupTestFile(store.Must(th.App.Srv.Store.FileInfo().Get(fileId)).(*model.FileInfo)); err != nil {
		t.Fatal(err)
	}

	th.cleanupTestFile(info)
}
