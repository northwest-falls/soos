package contenthash

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"io"
	"testing"
)

var vectors = []struct {
	name string
	data []byte
	want string
}{
	{
		name: "empty",
		data: []byte{},
		want: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	},
	{
		name: "small stays a plain sha256",
		data: []byte("hello"),
		want: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
	},
	{

		name: "exactly one slice",
		data: pattern(Slice, 7, 3),
		want: "67930bd55dbd6f8ce6d1ccf483b846c6f41cb480fcab7de24da712fe02abdc31",
	},
	{

		name: "one byte over a slice",
		data: pattern(Slice+1, 7, 3),
		want: "8e082887b6ddf384f3e044d3f72996d52352274f5b1877285f425c89033ec81a",
	},
	{
		name: "two slices and a partial",
		data: pattern(Slice*2+12345, 11, 5),
		want: "23ff8bb1a6076e96efbcd7ea306351c7e5680cecf0bc6c526226ab0648446ff0",
	},
}

func pattern(n, mul, add int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte((i*mul + add) & 0xFF)
	}
	return b
}

func TestMatchesBrowser(t *testing.T) {
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			got, err := Reader(bytes.NewReader(v.data), int64(len(v.data)))
			if err != nil {
				t.Fatalf("Reader: %v", err)
			}
			if got != v.want {
				t.Fatalf("hash mismatch\n got: %s\nwant: %s", got, v.want)
			}
		})
	}
}

func TestLengthSuffixIsLittleEndian(t *testing.T) {
	data := pattern(Slice+1, 7, 3)

	got, err := Reader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	outer := sha256.New()
	for off := 0; off < len(data); off += Slice {
		end := off + Slice
		if end > len(data) {
			end = len(data)
		}
		inner := sha256.Sum256(data[off:end])
		outer.Write(inner[:])
	}
	var be [8]byte
	binary.BigEndian.PutUint64(be[:], uint64(len(data)))
	outer.Write(be[:])
	wrong := hex.EncodeToString(outer.Sum(nil))

	if got == wrong {
		t.Fatal("length suffix is big endian; the browser writes it little endian")
	}
	if got != vectors[3].want {
		t.Fatalf("tree path changed:\n got: %s\nwant: %s", got, vectors[3].want)
	}
}

func TestStreamingIsChunkIndependent(t *testing.T) {
	data := pattern(Slice*2+9999, 3, 1)

	whole, err := Reader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	drip, err := Reader(&shortReader{data: data, chunk: 7777}, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}

	if whole != drip {
		t.Fatalf("chunking changed the hash:\n whole: %s\n drip:  %s", whole, drip)
	}
}

type shortReader struct {
	data  []byte
	chunk int
	pos   int
}

func (r *shortReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}

	n := r.chunk
	if n > len(p) {
		n = len(p)
	}
	if r.pos+n > len(r.data) {
		n = len(r.data) - r.pos
	}

	copy(p[:n], r.data[r.pos:r.pos+n])
	r.pos += n

	return n, nil
}

func BenchmarkTreePath(b *testing.B) {
	data := pattern(Slice*2+1, 3, 1)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := Reader(bytes.NewReader(data), int64(len(data))); err != nil {
			b.Fatal(err)
		}
	}
}
