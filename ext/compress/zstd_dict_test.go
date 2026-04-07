package compress_test

import (
	"strings"
	"testing"

	"github.com/jhonsferg/relay/ext/compress"
)

func TestZstdDictCompressor_CompressDecompress(t *testing.T) {
	t.Parallel()

	c, err := compress.NewZstdDictionaryCompressor(nil)
	if err != nil {
		t.Fatalf("NewZstdDictionaryCompressor: %v", err)
	}

	const want = "hello zstd world"
	compressed, err := c.Compress([]byte(want))
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	got, err := c.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if string(got) != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestZstdDictCompressor_NilDict(t *testing.T) {
	t.Parallel()

	c, err := compress.NewZstdDictionaryCompressor(nil)
	if err != nil {
		t.Fatalf("NewZstdDictionaryCompressor(nil): %v", err)
	}
	if c.Encoding() != "zstd" {
		t.Errorf("Encoding() = %q, want %q", c.Encoding(), "zstd")
	}

	const input = "nil dict fallback test"
	compressed, err := c.Compress([]byte(input))
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	out, err := c.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if string(out) != input {
		t.Errorf("round-trip mismatch: got %q, want %q", out, input)
	}
}

func TestZstdDictCompressor_LargePayload(t *testing.T) {
	t.Parallel()

	// 10 KB of repetitive text — zstd should compress this well.
	original := strings.Repeat("the quick brown fox jumps over the lazy dog\n", 250)

	c, err := compress.NewZstdDictionaryCompressor(nil)
	if err != nil {
		t.Fatalf("NewZstdDictionaryCompressor: %v", err)
	}

	compressed, err := c.Compress([]byte(original))
	if err != nil {
		t.Fatalf("Compress: %v", err)
	}
	if len(compressed) >= len(original) {
		t.Logf("compressed (%d) >= original (%d) — unexpected for repetitive input", len(compressed), len(original))
	}

	got, err := c.Decompress(compressed)
	if err != nil {
		t.Fatalf("Decompress: %v", err)
	}
	if string(got) != original {
		t.Errorf("decompressed content does not match original (lengths: got %d, want %d)", len(got), len(original))
	}
}
