// Copyright 2019 Oleg Sharoyko
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// TODO: High-level file comment.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/mail"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/badrpc/slog"
	"github.com/jhillyerd/go.enmime"
)

const (
	exOk       = 0
	exTempFail = 75
)

func main() {
	var destDir, syslogFacility string

	flag.StringVar(&destDir, "d", "", "Destination directory")
	flag.StringVar(&syslogFacility, "f", "", "Syslog facility")
	flag.Parse()

	if f, err := slog.ParseFacility(syslogFacility); err != nil {
		slog.Err(err)
		os.Exit(exTempFail)
	} else {
		slog.Init(slog.WithFacility(f))
	}

	os.Exit(process(os.Stdin, destDir))
}

func process(r io.Reader, destDir string) int {
	jobId := time.Now().Format("20060102-150405") + "-" + strconv.Itoa(os.Getpid())
	workDir := filepath.Join(destDir, jobId)

	slog.Info("Job ID: ", jobId)

	if err := os.Mkdir(workDir, os.FileMode(0700)); err != nil {
		slog.Err(err)
		return exTempFail
	}

	defer cleanup(workDir)

	msg, err := mail.ReadMessage(r)
	if err != nil {
		slog.Errf("mail.ReadMessage(): %v", err)
		return exTempFail
	}

	mime, err := enmime.ParseMIMEBody(msg)
	if err != nil {
		slog.Errf("enmime.ParseMIMEBody(): %v", err)
		return exTempFail
	}

	slog.Info("Message-Id: ", mime.GetHeader("Message-Id"))

	files := make([]string, 0, len(mime.Attachments))

	for i, a := range mime.Attachments {
		fn := jobId + "-" + fmt.Sprintf("%04d", i)
		if ofn := sanitizeFileName(a.FileName()); ofn != "" {
			fn += "-" + ofn
		}
		slog.Info("File: ", fn)
		files = append(files, fn)
		fp := filepath.Join(workDir, fn)
		// TODO(badrpc): think about filemode, maybe make it configurable via a parameter.
		w, err := os.Create(fp)
		if err != nil {
			slog.Errf("os.Create(%q): %v", fp, err)
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return exTempFail
		}
		if _, err := w.Write(a.Content()); err != nil {
			slog.Errf("Write: %v", err)
			return exTempFail
		}
		if err := w.Close(); err != nil {
			slog.Errf("Close: %v", err)
			return exTempFail
		}
	}

	for _, fn := range files {
		src := filepath.Join(workDir, fn)
		dst := filepath.Join(destDir, fn)
		if err := os.Link(src, dst); err != nil {
			slog.Err(err)
			return exTempFail
		}
		if err := os.Remove(src); err != nil {
			slog.Err(err)
			return exTempFail
		}
	}

	return exOk
}

// Allow no more than maxOriginalFileNameLenght characters from original file name.
const maxOriginalFileNameLength = 64

func sanitizeFileName(fn string) string {
	b := filepath.Base(fn)
	if len(b) > maxOriginalFileNameLength {
		return b[:maxOriginalFileNameLength/2-1] + ".." + b[len(b)-maxOriginalFileNameLength/2+1:]
	}
	return b
}

func cleanup(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Err(err)
	}
}
