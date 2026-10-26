package ffprobe

import (
	"testing"

	"maragu.dev/is"
)

func TestCappedBuffer(t *testing.T) {
	t.Run("should keep writes up to the cap", func(t *testing.T) {
		c := &cappedBuffer{max: 8}

		n, err := c.Write([]byte("four"))
		is.NotError(t, err)
		is.Equal(t, 4, n)
		_, _ = c.Write([]byte("more"))

		is.Equal(t, "fourmore", c.buf.String())
		is.True(t, !c.over, "over at the cap")
	})

	t.Run("should drop what goes over the cap and note it, without failing the write", func(t *testing.T) {
		c := &cappedBuffer{max: 8}

		_, _ = c.Write([]byte("four"))
		n, err := c.Write([]byte("and more"))
		is.NotError(t, err)
		is.Equal(t, 8, n)
		_, _ = c.Write([]byte("still more"))

		is.Equal(t, "fourand ", c.buf.String())
		is.True(t, c.over, "not over")
	})
}
