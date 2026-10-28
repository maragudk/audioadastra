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

// relabels are the MIME types a PDS may store a blob as instead of the type declared, by declared type,
// from reading the bytes itself. A PDS that reads nothing it knows keeps the declared type. The reference
// PDS reads with the file-type library: Opus in WebM comes back as video/webm, M4A as audio/x-m4a or,
// without the M4A brand, video/mp4, and older releases use older names, such as audio/x-flac.
var relabels = map[string][]string{
	"audio/aac":        {"audio/x-aac"},
	"audio/aiff":       {"audio/x-aiff"},
	"audio/flac":       {"audio/x-flac"},
	"audio/mp4":        {"audio/x-m4a", "video/mp4"},
	"audio/ogg":        {"audio/opus"},
	"audio/wav":        {"audio/vnd.wave", "audio/wave", "audio/x-wav"},
	"audio/webm":       {"video/webm"},
	"audio/x-matroska": {"video/matroska", "video/x-matroska"},
	"audio/x-wavpack":  {"audio/wavpack"},
}

// Probe the file at the given path. It is audio when it is in one of the supported formats, has an
// audio stream with a codec, channels and a sample rate, and no video stream other than cover art.
//
// Only the fields it reads are asked of ffprobe, so the file's metadata tags, which can be any size,
// are never printed, and ffprobe's output is cut off at a size no real audio file needs: a file whose
// output goes over it is refused.
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
		"-show_entries", "stream=codec_type,codec_name,channels,sample_rate:stream_disposition=attached_pic:format=format_name,duration",
		"file:"+path,
	)
	stdout, stderr := &cappedBuffer{max: 256 * 1024}, &cappedBuffer{max: 16 * 1024}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	// Run returns this long after ffprobe is killed, even if something still holds its output open.
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return model.AudioInfo{}, fmt.Errorf("running ffprobe: %w", ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return model.AudioInfo{}, fmt.Errorf("%w: ffprobe: %v", model.ErrorNotAudio, firstLine(stderr.buf.String()))
		}
		return model.AudioInfo{}, fmt.Errorf("running ffprobe: %w", err)
	}

	if stdout.over {
		return model.AudioInfo{}, fmt.Errorf("%w: ffprobe output is over %v bytes", model.ErrorNotAudio, stdout.max)
	}
	var out output
	if err := json.Unmarshal(stdout.buf.Bytes(), &out); err != nil {
		return model.AudioInfo{}, fmt.Errorf("reading ffprobe output: %w", err)
	}
	return out.audioInfo()
}

// output of ffprobe with the entries [Prober.Probe] asks for.
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

	info.StoredMIMETypes = append([]string{info.MIMEType}, relabels[info.MIMEType]...)
	// WebM is the Matroska subset for Opus and Vorbis, so such audio is declared as WebM. ffprobe reads
	// WebM and Matroska alike, so the file may be either, and a PDS that reads its doctype may store it as
	// Matroska.
	if demuxer == "matroska" && (info.Codec == "opus" || info.Codec == "vorbis") {
		info.MIMEType = "audio/webm"
		info.StoredMIMETypes = slices.Concat([]string{info.MIMEType}, relabels[info.MIMEType], relabels["audio/x-matroska"])
	}
	return info, nil
}

// cappedBuffer keeps what is written to it up to max bytes, and drops the rest, noting that there was
// more. A write never fails, so a writer is never stopped short.
type cappedBuffer struct {
	buf  bytes.Buffer
	max  int
	over bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if room := c.max - c.buf.Len(); len(p) > room {
		c.buf.Write(p[:max(room, 0)])
		c.over = true
		return len(p), nil
	}
	return c.buf.Write(p)
}

// firstLine of ffprobe's error output, which names what it failed on, cut short.
func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}
