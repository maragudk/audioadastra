package ffprobe_test

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"maragu.dev/is"

	"app/audiotest"
	"app/ffprobe"
	"app/model"
)

func TestProber_Probe(t *testing.T) {
	t.Run("should read the format and audio stream of a FLAC file", func(t *testing.T) {
		p := newProber(t)

		info, err := p.Probe(t.Context(), audiotest.WriteFile(t, "tone.flac", audiotest.FLAC))
		is.NotError(t, err)
		is.Equal(t, "flac", info.Format)
		is.Equal(t, "flac", info.Codec)
		is.Equal(t, "audio/flac", info.MIMEType)
		is.Equal(t, 1, info.Channels)
		is.Equal(t, 8000, info.SampleRate)
		is.Equal(t, 2*time.Second, info.Duration)
	})

	// Each file is made from the FLAC with ffmpeg, which is installed wherever ffprobe is.
	tests := []struct {
		name string
		file string
		args []string
		mime string
	}{
		{name: "should declare WAV as audio/wav", file: "tone.wav", mime: "audio/wav"},
		{name: "should declare MP3 as audio/mpeg", file: "tone.mp3", mime: "audio/mpeg"},
		{name: "should declare Ogg Opus as audio/ogg", file: "tone.opus", args: []string{"-c:a", "libopus", "-ar", "48000"}, mime: "audio/ogg"},
		{name: "should declare AAC in MP4 as audio/mp4", file: "tone.m4a", args: []string{"-c:a", "aac"}, mime: "audio/mp4"},
		{name: "should declare ADTS AAC as audio/aac", file: "tone.aac", args: []string{"-c:a", "aac"}, mime: "audio/aac"},
		{name: "should declare AIFF as audio/aiff", file: "tone.aiff", mime: "audio/aiff"},
		{name: "should declare Opus in WebM as audio/webm", file: "tone.webm", args: []string{"-c:a", "libopus", "-ar", "48000"}, mime: "audio/webm"},
		{name: "should declare FLAC in Matroska as audio/x-matroska", file: "tone.mka", args: []string{"-c:a", "flac"}, mime: "audio/x-matroska"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p := newProber(t)
			path := convert(t, test.file, test.args...)

			info, err := p.Probe(t.Context(), path)
			is.NotError(t, err)
			is.Equal(t, test.mime, info.MIMEType)
			is.Equal(t, 1, info.Channels)
			is.True(t, info.Duration > time.Second, info.Duration.String())
		})
	}

	t.Run("should accept an MP3 with cover art", func(t *testing.T) {
		p := newProber(t)
		cover := filepath.Join(t.TempDir(), "cover.png")
		ffmpeg(t, "-f", "lavfi", "-i", "color=c=pink:s=16x16", "-frames:v", "1", cover)
		path := filepath.Join(t.TempDir(), "covered.mp3")
		ffmpeg(t, "-i", audiotest.WriteFile(t, "tone.flac", audiotest.FLAC), "-i", cover, "-map", "0", "-map", "1",
			"-c:v", "mjpeg", "-disposition:v", "attached_pic", path)

		info, err := p.Probe(t.Context(), path)
		is.NotError(t, err)
		is.Equal(t, "audio/mpeg", info.MIMEType)
	})

	t.Run("should refuse a text file as not audio", func(t *testing.T) {
		p := newProber(t)

		_, err := p.Probe(t.Context(), audiotest.WriteFile(t, "notes.txt", []byte("Not a song, just some notes.\n")))
		is.Error(t, model.ErrorNotAudio, err)
	})

	t.Run("should refuse an image as not audio", func(t *testing.T) {
		p := newProber(t)
		path := filepath.Join(t.TempDir(), "image.png")
		ffmpeg(t, "-f", "lavfi", "-i", "color=c=pink:s=16x16", "-frames:v", "1", path)

		_, err := p.Probe(t.Context(), path)
		is.Error(t, model.ErrorNotAudio, err)
	})

	t.Run("should refuse a video with sound as not audio", func(t *testing.T) {
		p := newProber(t)
		path := filepath.Join(t.TempDir(), "video.mp4")
		ffmpeg(t, "-f", "lavfi", "-i", "color=c=pink:s=16x16:d=1", "-i", audiotest.WriteFile(t, "tone.flac", audiotest.FLAC),
			"-c:a", "aac", "-shortest", path)

		_, err := p.Probe(t.Context(), path)
		is.Error(t, model.ErrorNotAudio, err)
	})

	t.Run("should refuse audio in a format it has no MIME type for as not audio", func(t *testing.T) {
		p := newProber(t)

		_, err := p.Probe(t.Context(), convert(t, "tone.voc"))
		is.Error(t, model.ErrorNotAudio, err)
	})

	t.Run("should refuse a playlist without reading it as one", func(t *testing.T) {
		p := newProber(t)
		audio := audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)
		playlist := audiotest.WriteFile(t, "list.m3u8", []byte("#EXTM3U\n#EXT-X-TARGETDURATION:2\n#EXTINF:2,\nfile:"+audio+"\n#EXT-X-ENDLIST\n"))

		_, err := p.Probe(t.Context(), playlist)
		is.Error(t, model.ErrorNotAudio, err)
		// Read as a playlist, it would have opened the file it names; ffprobe must refuse the demuxer.
		is.True(t, strings.Contains(err.Error(), "whitelist"), err.Error())
	})
}

func newProber(t *testing.T) *ffprobe.Prober {
	t.Helper()

	p, err := ffprobe.NewProber()
	if err != nil {
		t.Fatalf("ffprobe is required, from ffmpeg: %v", err)
	}
	return p
}

// convert the FLAC tone to a file with the given name, whose extension picks the format, with extra
// ffmpeg output arguments.
func convert(t *testing.T, name string, args ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	ffmpeg(t, append(append([]string{"-i", audiotest.WriteFile(t, "tone.flac", audiotest.FLAC)}, args...), path)...)
	return path
}

func ffmpeg(t *testing.T, args ...string) {
	t.Helper()

	out, err := exec.Command("ffmpeg", append([]string{"-hide_banner", "-loglevel", "error", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v: %s", args, err, out)
	}
}
