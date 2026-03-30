package relay

import "io"

// ProgressFunc is called periodically during upload or download with the
// number of bytes transferred so far and the total size. If the total is
// unknown (no Content-Length header), total is -1.
type ProgressFunc func(transferred, total int64)

// progressReader wraps an io.Reader and calls fn after each Read.
type progressReader struct {
	r           io.Reader
	total       int64
	transferred int64
	fn          ProgressFunc
}

func newProgressReader(r io.Reader, total int64, fn ProgressFunc) io.Reader {
	return &progressReader{r: r, total: total, fn: fn}
}

func (p *progressReader) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.transferred += int64(n)
		p.fn(p.transferred, p.total)
	}
	return n, err
}

// progressReadCloser wraps an io.ReadCloser for download progress tracking.
type progressReadCloser struct {
	rc          io.ReadCloser
	total       int64
	transferred int64
	fn          ProgressFunc
}

func newProgressReadCloser(rc io.ReadCloser, total int64, fn ProgressFunc) io.ReadCloser {
	return &progressReadCloser{rc: rc, total: total, fn: fn}
}

func (p *progressReadCloser) Read(buf []byte) (int, error) {
	n, err := p.rc.Read(buf)
	if n > 0 {
		p.transferred += int64(n)
		p.fn(p.transferred, p.total)
	}
	return n, err
}

func (p *progressReadCloser) Close() error { return p.rc.Close() }
