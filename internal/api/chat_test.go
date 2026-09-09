package api

import (
	"bytes"
	"mime/multipart"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

func multipartChatContext(t *testing.T, write func(*multipart.Writer)) *app.RequestContext {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	write(w)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	c := app.NewContext(0)
	c.Request.Header.SetContentTypeBytes([]byte(w.FormDataContentType()))
	c.Request.SetBody(body.Bytes())
	return c
}

func writeMultipartField(t *testing.T, w *multipart.Writer, key, value string) {
	t.Helper()
	if err := w.WriteField(key, value); err != nil {
		t.Fatal(err)
	}
}

func writeMultipartPNG(t *testing.T, w *multipart.Writer, key string) {
	t.Helper()
	part, err := w.CreateFormFile(key, "screenshot.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("\x89PNG\r\n\x1a\nfixture")); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeChatMultipartWithImage(t *testing.T) {
	t.Parallel()
	c := multipartChatContext(t, func(w *multipart.Writer) {
		writeMultipartField(t, w, "message", "看下这张截图")
		writeMultipartField(t, w, "thread_id", "thread-1")
		writeMultipartField(t, w, "turn_id", "turn-1")
		writeMultipartField(t, w, "page_context", `{"active_key":"okr","selection":{"kind":"project","id":7,"label":"Emily"},"view_state":{"tab":"weekly-fill"}}`)
		writeMultipartPNG(t, w, "image")
	})
	req, cleanup, err := decodeChatMultipart(c)
	if err != nil {
		t.Fatalf("decodeChatMultipart() error = %v", err)
	}
	if req.Message != "看下这张截图" || req.ThreadID != "thread-1" || req.TurnID != "turn-1" {
		t.Fatalf("request = %#v", req)
	}
	if req.PageContext == nil || req.PageContext.ActiveKey != "okr" || req.PageContext.Selection == nil || req.PageContext.Selection.ID != 7 {
		t.Fatalf("page context = %#v", req.PageContext)
	}
	if req.ImagePath == "" {
		t.Fatal("image path is empty")
	}
	if _, err := os.Stat(req.ImagePath); err != nil {
		t.Fatalf("temporary image missing: %v", err)
	}
	cleanup()
	if _, err := os.Stat(req.ImagePath); !os.IsNotExist(err) {
		t.Fatalf("temporary image still exists after cleanup: %v", err)
	}
}

func TestDecodeChatMultipartRejectsInvalidContract(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		write   func(*testing.T, *multipart.Writer)
		wantErr string
	}{
		{
			name: "unknown field",
			write: func(t *testing.T, w *multipart.Writer) {
				writeMultipartField(t, w, "message", "hello")
				writeMultipartField(t, w, "extra", "nope")
			},
			wantErr: "unknown chat form field",
		},
		{
			name: "invalid page context",
			write: func(t *testing.T, w *multipart.Writer) {
				writeMultipartField(t, w, "message", "hello")
				writeMultipartField(t, w, "page_context", `{"active_key":"okr","unknown":true}`)
			},
			wantErr: "unknown field",
		},
		{
			name: "invalid turn id",
			write: func(t *testing.T, w *multipart.Writer) {
				writeMultipartField(t, w, "message", "hello")
				writeMultipartField(t, w, "turn_id", "bad/turn")
			},
			wantErr: "turn_id is invalid",
		},
		{
			name: "multiple images",
			write: func(t *testing.T, w *multipart.Writer) {
				writeMultipartField(t, w, "message", "hello")
				writeMultipartPNG(t, w, "image")
				writeMultipartPNG(t, w, "image")
			},
			wantErr: "at most once",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := multipartChatContext(t, func(w *multipart.Writer) { test.write(t, w) })
			_, _, err := decodeChatMultipart(c)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want %q", err, test.wantErr)
			}
		})
	}
}
