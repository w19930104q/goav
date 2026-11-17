// Use of this source code is governed by a MIT license that can be found in the LICENSE file.

package avutil

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

// Fixed geometry: odd width and odd height on purpose, with padded strides
// so that linesize != width and chroma planes carry padding too.
const (
	testW       = 5
	testH       = 5
	testYStride = 8 // 5 pixels + 3 padding
	testCW      = (testW + 1) / 2
	testCH      = (testH + 1) / 2
	testCStride = 4 // 3 pixels + 1 padding
)

const pixFmtYUV420P = 0 // AV_PIX_FMT_YUV420P

// Plane layout inside the synthetic frame buffer.
const (
	yuvOffY  = 0
	yuvOffCb = testYStride * testH
	yuvOffCr = testYStride*testH + testCStride*testCH
	yuvSize  = testYStride*testH + 2*testCStride*testCH
)

// px is the deterministic pixel pattern of the synthetic frames.
func px(plane, x, y int) uint8 {
	return uint8((plane*61 + y*17 + x*31 + 7) % 251)
}

// cBytes exposes n bytes of C memory as a Go slice (the backing memory is
// allocated with AvMalloc and freed by the caller).
func cBytes(p unsafe.Pointer, n int) []byte {
	h := reflect.SliceHeader{Data: uintptr(p), Len: n, Cap: n}
	return *(*[]byte)(unsafe.Pointer(&h))
}

// setPlaneData sets f.data[i] to p. The C pointer type cannot be named from
// a non-cgo test file, so the slot is written through unsafe.Pointer.
func setPlaneData(f *Frame, i int, p unsafe.Pointer) {
	*(*unsafe.Pointer)(unsafe.Pointer(&f.data[i])) = p
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// refPlane renders the tightly packed, top-down reference content of a plane.
func refPlane(plane, rows, cols int) []byte {
	out := make([]byte, rows*cols)
	for y := 0; y < rows; y++ {
		for x := 0; x < cols; x++ {
			out[y*cols+x] = px(plane, x, y)
		}
	}
	return out
}

// writePlane stamps the reference content of a plane into a padded frame
// buffer. When bottomUp is set, image row y is stored at buffer row
// rows-1-y, mimicking a negative-stride (bottom-up) FFmpeg frame.
func writePlane(dst []byte, stride, rows, cols, plane int, bottomUp bool) {
	for y := 0; y < rows; y++ {
		sy := y
		if bottomUp {
			sy = rows - 1 - y
		}
		for x := 0; x < cols; x++ {
			dst[sy*stride+x] = px(plane, x, y)
		}
	}
}

// buildYUVFrame allocates a synthetic 4:2:0 frame in C memory. Padding
// bytes are filled with a 0xAA sentinel so any padding leak into the
// decoded image changes its hash. When bottomUp is set, all planes are
// stored with negative strides and data[i] points at the last row.
func buildYUVFrame(t *testing.T, bottomUp bool) (*Frame, unsafe.Pointer) {
	t.Helper()
	buf := AvMalloc(uintptr(yuvSize))
	if buf == nil {
		t.Fatal("AvMalloc failed")
	}
	b := cBytes(buf, yuvSize)
	for i := range b {
		b[i] = 0xAA
	}
	writePlane(b[yuvOffY:], testYStride, testH, testW, 0, bottomUp)
	writePlane(b[yuvOffCb:], testCStride, testCH, testCW, 1, bottomUp)
	writePlane(b[yuvOffCr:], testCStride, testCH, testCW, 2, bottomUp)

	f := AvFrameAlloc()
	f.width = testW
	f.height = testH
	f.format = pixFmtYUV420P
	base := uintptr(buf)
	if bottomUp {
		// Bottom-up: data[i] addresses the LAST row, stride is negative.
		setPlaneData(f, 0, unsafe.Pointer(base+yuvOffY+(testH-1)*testYStride))
		setPlaneData(f, 1, unsafe.Pointer(base+yuvOffCb+(testCH-1)*testCStride))
		setPlaneData(f, 2, unsafe.Pointer(base+yuvOffCr+(testCH-1)*testCStride))
		f.linesize[0] = -testYStride
		f.linesize[1] = -testCStride
		f.linesize[2] = -testCStride
	} else {
		setPlaneData(f, 0, unsafe.Pointer(base+yuvOffY))
		setPlaneData(f, 1, unsafe.Pointer(base+yuvOffCb))
		setPlaneData(f, 2, unsafe.Pointer(base+yuvOffCr))
		f.linesize[0] = testYStride
		f.linesize[1] = testCStride
		f.linesize[2] = testCStride
	}
	return f, buf
}

func TestGetPictureStrideAndOrientation(t *testing.T) {
	wantY := sha(refPlane(0, testH, testW))
	wantCb := sha(refPlane(1, testCH, testCW))
	wantCr := sha(refPlane(2, testCH, testCW))

	for _, bottomUp := range []bool{false, true} {
		name := "top-down"
		if bottomUp {
			name = "bottom-up"
		}
		t.Run(name, func(t *testing.T) {
			f, buf := buildYUVFrame(t, bottomUp)
			defer AvFrameFree(f)
			defer AvFree(buf)

			img, err := GetPicture(f)
			if err != nil {
				t.Fatalf("GetPicture: %v", err)
			}
			if got := img.Rect.Dx(); got != testW {
				t.Errorf("width = %d, want %d (linesize padding leaked into picture size)", got, testW)
			}
			if got := img.Rect.Dy(); got != testH {
				t.Errorf("height = %d, want %d", got, testH)
			}
			if got := sha(img.Y); got != wantY {
				t.Errorf("Y plane hash = %s, want %s", got, wantY)
			}
			if got := sha(img.Cb); got != wantCb {
				t.Errorf("Cb plane hash = %s, want %s", got, wantCb)
			}
			if got := sha(img.Cr); got != wantCr {
				t.Errorf("Cr plane hash = %s, want %s", got, wantCr)
			}
		})
	}
}

func TestDataAndLinesizeExposeNegativeStride(t *testing.T) {
	f, buf := buildYUVFrame(t, true)
	defer AvFrameFree(f)
	defer AvFree(buf)

	ls := Linesize(f)
	wantStrides := [3]int32{-testYStride, -testCStride, -testCStride}
	for p := 0; p < 3; p++ {
		if ls[p] != wantStrides[p] {
			t.Errorf("Linesize[%d] = %d, want %d", p, ls[p], wantStrides[p])
		}
	}
	d := Data(f)
	wantRows := [3]uintptr{(testH - 1) * testYStride, (testCH - 1) * testCStride, (testCH - 1) * testCStride}
	wantOffs := [3]uintptr{yuvOffY, yuvOffCb, yuvOffCr}
	for p := 0; p < 3; p++ {
		// Bottom-up planes: data[p] must address the LAST row.
		want := uintptr(buf) + wantOffs[p] + wantRows[p]
		if got := uintptr(unsafe.Pointer(d[p])); got != want {
			t.Errorf("Data[%d] = %#x, want last-row pointer %#x", p, got, want)
		}
	}
}

func TestPlaneExtentStaysInsideBuffer(t *testing.T) {
	rows := [3]int{testH, testCH, testCH}
	strides := [3]int{testYStride, testCStride, testCStride}
	for _, bottomUp := range []bool{false, true} {
		f, buf := buildYUVFrame(t, bottomUp)
		lo0 := uintptr(buf)
		hi0 := lo0 + uintptr(yuvSize)
		for p := 0; p < 3; p++ {
			lo, hi, ok := planeExtent(f, p, rows[p])
			if !ok {
				t.Fatalf("bottomUp=%v plane %d: extent not ok", bottomUp, p)
			}
			if lo < lo0 || hi > hi0 {
				t.Errorf("bottomUp=%v plane %d: extent [%#x, %#x) escapes buffer [%#x, %#x)",
					bottomUp, p, lo, hi, lo0, hi0)
			}
			// The extent must cover exactly rows * |stride| bytes.
			if got, want := hi-lo, uintptr(rows[p]*strides[p]); got != want {
				t.Errorf("bottomUp=%v plane %d: extent size = %d, want %d", bottomUp, p, got, want)
			}
		}
		AvFrameFree(f)
		AvFree(buf)
	}
}

func TestGetPictureRGBStrideAndOrientation(t *testing.T) {
	const rgbaStride = 4*testW + 4 // 4 bytes of padding per row
	const bufSize = rgbaStride * testH

	// Packed 4-byte pixel pattern.
	ref := make([]byte, 4*testW*testH)
	for y := 0; y < testH; y++ {
		for x := 0; x < testW; x++ {
			for c := 0; c < 4; c++ {
				ref[(y*testW+x)*4+c] = uint8((x*13 + y*29 + c*47 + 3) % 256)
			}
		}
	}
	want := sha(ref)

	for _, bottomUp := range []bool{false, true} {
		name := "top-down"
		if bottomUp {
			name = "bottom-up"
		}
		t.Run(name, func(t *testing.T) {
			buf := AvMalloc(uintptr(bufSize))
			if buf == nil {
				t.Fatal("AvMalloc failed")
			}
			defer AvFree(buf)
			b := cBytes(buf, bufSize)
			for i := range b {
				b[i] = 0xAA
			}
			for y := 0; y < testH; y++ {
				sy := y
				if bottomUp {
					sy = testH - 1 - y
				}
				copy(b[sy*rgbaStride:sy*rgbaStride+4*testW], ref[y*4*testW:(y+1)*4*testW])
			}

			f := AvFrameAlloc()
			defer AvFrameFree(f)
			f.width = testW
			f.height = testH
			addr := uintptr(buf)
			if bottomUp {
				setPlaneData(f, 0, unsafe.Pointer(addr+(testH-1)*rgbaStride))
				f.linesize[0] = -rgbaStride
			} else {
				setPlaneData(f, 0, unsafe.Pointer(addr))
				f.linesize[0] = rgbaStride
			}

			img, err := GetPictureRGB(f)
			if err != nil {
				t.Fatalf("GetPictureRGB: %v", err)
			}
			if img.Stride != 4*testW {
				t.Errorf("RGBA stride = %d, want %d", img.Stride, 4*testW)
			}
			if got := sha(img.Pix); got != want {
				t.Errorf("RGBA hash = %s, want %s", got, want)
			}
		})
	}
}

func TestGetPictureOutOfBounds(t *testing.T) {
	// A real buffer-backed frame so bounds are validated against buf[].
	f := AvFrameAlloc()
	if err := AvSetFrame(f, 64, 64, pixFmtYUV420P); err != nil {
		t.Fatalf("AvSetFrame: %v", err)
	}
	defer AvFrameFree(f)

	if _, err := GetPicture(f); err != nil {
		t.Fatalf("GetPicture on valid frame: %v", err)
	}

	// Data pointer pushed past the end of the backing buffer.
	orig := f.data[0]
	base := uintptr(unsafe.Pointer(f.buf[0].data))
	setPlaneData(f, 0, unsafe.Pointer(base+uintptr(f.buf[0].size)+4096))
	if _, err := GetPicture(f); err == nil {
		t.Error("GetPicture with out-of-buffer data pointer: want error, got nil")
	} else if !strings.Contains(err.Error(), "outside") {
		t.Errorf("unexpected error: %v", err)
	}
	f.data[0] = orig

	// Stride inflated so the plane extent overflows the buffer.
	origLs := f.linesize[0]
	f.linesize[0] = 1 << 20
	if _, err := GetPicture(f); err == nil {
		t.Error("GetPicture with overflowing stride: want error, got nil")
	}
	f.linesize[0] = origLs

	// No buffer at all behind the data pointers.
	bare := AvFrameAlloc()
	defer AvFrameFree(bare)
	bare.width = 64
	bare.height = 64
	if _, err := GetPicture(bare); err == nil {
		t.Error("GetPicture with nil data pointers: want error, got nil")
	}

	// Degenerate geometry.
	zero := AvFrameAlloc()
	defer AvFrameFree(zero)
	if _, err := GetPicture(zero); err == nil {
		t.Error("GetPicture with 0x0 frame: want error, got nil")
	}
	if _, err := GetPictureRGB(zero); err == nil {
		t.Error("GetPictureRGB with 0x0 frame: want error, got nil")
	}
}

func TestGetPictureRGBRejectsNarrowStride(t *testing.T) {
	f, buf := buildYUVFrame(t, false) // Y plane stride 8 < 4*5 bytes/row
	defer AvFrameFree(f)
	defer AvFree(buf)
	if _, err := GetPictureRGB(f); err == nil {
		t.Error("GetPictureRGB with stride < 4*width: want error, got nil")
	}
}

func TestCopyPlaneRejectsSmallDestination(t *testing.T) {
	f, buf := buildYUVFrame(t, false)
	defer AvFrameFree(f)
	defer AvFree(buf)
	if err := copyPlane(make([]byte, 4), 4, f, 0, testW, testH); err == nil {
		t.Error("copyPlane into undersized dst: want error, got nil")
	}
}
