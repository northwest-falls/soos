package contenthash

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
)

// Wire format shared with the browser. Slice size and the little-endian
// length suffix must not change, or dedup silently breaks on every real file.
const Slice = 8 * 1024 * 1024

const bufSize = 1024 * 1024

func Reader(r io.Reader, size int64) (string, error) {
	buf := make([]byte, bufSize)

	if size <= Slice {
		h := sha256.New()
		if _, err := io.CopyBuffer(h, io.LimitReader(r, size), buf); err != nil {
			return "", err
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	outer := sha256.New()

	for remaining := size; remaining > 0; {
		n := int64(Slice)
		if remaining < n {
			n = remaining
		}

		inner := sha256.New()
		if _, err := io.CopyBuffer(inner, io.LimitReader(r, n), buf); err != nil {
			return "", err
		}
		outer.Write(inner.Sum(nil))

		remaining -= n
	}

	var length [8]byte
	// Little endian. Go's instinct is BigEndian and it would be wrong.
	binary.LittleEndian.PutUint64(length[:], uint64(size))
	outer.Write(length[:])

	return hex.EncodeToString(outer.Sum(nil)), nil
}

func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		return "", err
	}

	return Reader(f, st.Size())
}
