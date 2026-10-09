// Package ffprobe finds out whether a file is audio, and what kind, by running ffprobe from FFmpeg on
// it as a separate process.
package ffprobe

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"app/model"
)

// Prober of audio files. Safe for concurrent use.
type Prober struct {
	path string
}

// NewProber with the ffprobe on the PATH, which it fails without.
func NewProber() (*Prober, error) {
	path, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("finding ffprobe: %w", err)
	}
	return &Prober{path: path}, nil
}

// formats ffprobe may read a file as, by demuxer name, with the MIME type to declare for each. The
// first name in ffprobe's format_name for a file is its demuxer.
var formats = map[string]string{
	"aac":      "audio/aac",
	"aiff":     "audio/aiff",
	"caf":      "audio/x-caf",
	"flac":     "audio/flac",
	"matroska": "audio/x-matroska",
	"mov":      "audio/mp4",
	"mp3":      "audio/mpeg",
	"ogg":      "audio/ogg",
	"wav":      "audio/wav",
	"wv":       "audio/x-wavpack",
}

// Probe the file at the given path. It is audio when it is in one of the supported formats, has an
// audio stream with a codec, channels and a sample rate, and no video stream other than cover art.
//
// The error is [model.ErrorNotAudio] when the file is not audio, or in a format there is no MIME type
// for.
func (p *Prober) Probe(ctx context.Context, path string) (model.AudioInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Files are untrusted. The file: prefix keeps a path from being read as another protocol's URL, and
	// the whitelists keep ffprobe to reading that one local file with the demuxers in formats, since
	// others, such as those for playlists, would open other files the file names.
	cmd := exec.CommandContext(ctx, p.path,
		"-v", "error",
		"-protocol_whitelist", "file",
		"-format_whitelist", strings.Join(slices.Sorted(maps.Keys(formats)), ","),
		"-print_format", "json",
		"-show_format", "-show_streams",
		"file:"+path,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	// Run returns this long after ffprobe is killed, even if something still holds its output open.
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return model.AudioInfo{}, fmt.Errorf("running ffprobe: %w", ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return model.AudioInfo{}, fmt.Errorf("%w: ffprobe: %v", model.ErrorNotAudio, firstLine(stderr.String()))
		}
		return model.AudioInfo{}, fmt.Errorf("running ffprobe: %w", err)
	}

	var out output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		return model.AudioInfo{}, fmt.Errorf("reading ffprobe output: %w", err)
	}
	return out.audioInfo()
}

// output of ffprobe with -show_format and -show_streams, as far as it is read.
type output struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Channels    int    `json:"channels"`
		SampleRate  string `json:"sample_rate"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

func (o output) audioInfo() (model.AudioInfo, error) {
	demuxer, _, _ := strings.Cut(o.Format.FormatName, ",")
	mimeType, ok := formats[demuxer]
	if !ok {
		return model.AudioInfo{}, fmt.Errorf("%w: format %v is not supported", model.ErrorNotAudio, o.Format.FormatName)
	}

	info := model.AudioInfo{Format: o.Format.FormatName, MIMEType: mimeType}
	if seconds, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil && seconds > 0 {
		info.Duration = time.Duration(seconds * float64(time.Second))
	}

	for _, s := range o.Streams {
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic != 1 {
				return model.AudioInfo{}, fmt.Errorf("%w: has a %v video stream", model.ErrorNotAudio, s.CodecName)
			}
		case "audio":
			sampleRate, _ := strconv.Atoi(s.SampleRate)
			if info.Codec != "" || s.CodecName == "" || s.Channels <= 0 || sampleRate <= 0 {
				continue
			}
			info.Codec, info.Channels, info.SampleRate = s.CodecName, s.Channels, sampleRate
		}
	}
	if info.Codec == "" {
		return model.AudioInfo{}, fmt.Errorf("%w: no audio stream with a codec, channels and a sample rate", model.ErrorNotAudio)
	}

	// WebM is the Matroska subset for Opus and Vorbis, so such audio is declared as WebM.
	if demuxer == "matroska" && (info.Codec == "opus" || info.Codec == "vorbis") {
		info.MIMEType = "audio/webm"
	}
	return info, nil
}

// firstLine of ffprobe's error output, which names what it failed on, cut short.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}
