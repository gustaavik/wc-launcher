package install

import (
	"bytes"
	"io"
	"time"
)

// zeroTime disables http.ServeContent's modtime handling in tests.
var zeroTime = time.Time{}

func newReadSeeker(b []byte) io.ReadSeeker { return bytes.NewReader(b) }
