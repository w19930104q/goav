// Use of this source code is governed by a MIT license that can be found in the LICENSE file.
// Giorgis (habtom@giorgis.io)

package avutil

/*
	#cgo pkg-config: libavutil
	#include <libavutil/frame.h>
	#include <stdlib.h>
*/
import "C"
import (
	"fmt"
	"image"
	"unsafe"
)

type (
	AvBuffer            C.struct_AVBuffer
	AvBufferRef         C.struct_AVBufferRef
	AvBufferPool        C.struct_AVBufferPool
	Frame               C.struct_AVFrame
	AvFrameSideData     C.struct_AVFrameSideData
	AvFrameSideDataType C.enum_AVFrameSideDataType
)

func AvprivFrameGetMetadatap(f *Frame) *Dictionary {
	return (*Dictionary)(unsafe.Pointer(f.metadata))
}

func AvFrameSetQpTable(f *Frame, b *AvBufferRef, s, q int) int {
	return int(C.av_frame_set_qp_table((*C.struct_AVFrame)(unsafe.Pointer(f)), (*C.struct_AVBufferRef)(unsafe.Pointer(b)), C.int(s), C.int(q)))
}

func AvFrameGetQpTable(f *Frame, s, t *int) int8 {
	return int8(*C.av_frame_get_qp_table((*C.struct_AVFrame)(unsafe.Pointer(f)), (*C.int)(unsafe.Pointer(s)), (*C.int)(unsafe.Pointer(t))))
}

//Allocate an Frame and set its fields to default values.
func AvFrameAlloc() *Frame {
	return (*Frame)(unsafe.Pointer(C.av_frame_alloc()))
}

//Free the frame and any dynamically allocated objects in it, e.g.
func AvFrameFree(f *Frame) {
	C.av_frame_free((**C.struct_AVFrame)(unsafe.Pointer(&f)))
}

//Allocate new buffer(s) for audio or video data.
func AvFrameGetBuffer(f *Frame, a int) int {
	return int(C.av_frame_get_buffer((*C.struct_AVFrame)(unsafe.Pointer(f)), C.int(a)))
}

//Setup a new reference to the data described by an given frame.
func AvFrameRef(d, s *Frame) int {
	return int(C.av_frame_ref((*C.struct_AVFrame)(unsafe.Pointer(d)), (*C.struct_AVFrame)(unsafe.Pointer(s))))
}

//Create a new frame that references the same data as src.
func AvFrameClone(f *Frame) *Frame {
	return (*Frame)(C.av_frame_clone((*C.struct_AVFrame)(unsafe.Pointer(f))))
}

//Unreference all the buffers referenced by frame and reset the frame fields.
func AvFrameUnref(f *Frame) {
	cf := (*C.struct_AVFrame)(unsafe.Pointer(f))
	C.av_frame_unref(cf)
}

//Move everythnig contained in src to dst and reset src.
func AvFrameMoveRef(d, s *Frame) {
	C.av_frame_move_ref((*C.struct_AVFrame)(unsafe.Pointer(d)), (*C.struct_AVFrame)(unsafe.Pointer(s)))
}

//Check if the frame data is writable.
func AvFrameIsWritable(f *Frame) int {
	return int(C.av_frame_is_writable((*C.struct_AVFrame)(unsafe.Pointer(f))))
}

//Ensure that the frame data is writable, avoiding data copy if possible.
func AvFrameMakeWritable(f *Frame) int {
	return int(C.av_frame_make_writable((*C.struct_AVFrame)(unsafe.Pointer(f))))
}

//Copy only "metadata" fields from src to dst.
func AvFrameCopyProps(d, s *Frame) int {
	return int(C.av_frame_copy_props((*C.struct_AVFrame)(unsafe.Pointer(d)), (*C.struct_AVFrame)(unsafe.Pointer(s))))
}

//Get the buffer reference a given data plane is stored in.
func AvFrameGetPlaneBuffer(f *Frame, p int) *AvBufferRef {
	return (*AvBufferRef)(C.av_frame_get_plane_buffer((*C.struct_AVFrame)(unsafe.Pointer(f)), C.int(p)))
}

//Add a new side data to a frame.
func AvFrameNewSideData(f *Frame, d AvFrameSideDataType, s int) *AvFrameSideData {
	return (*AvFrameSideData)(C.av_frame_new_side_data((*C.struct_AVFrame)(unsafe.Pointer(f)), (C.enum_AVFrameSideDataType)(d), C.int(s)))
}

func AvFrameGetSideData(f *Frame, t AvFrameSideDataType) *AvFrameSideData {
	return (*AvFrameSideData)(C.av_frame_get_side_data((*C.struct_AVFrame)(unsafe.Pointer(f)), (C.enum_AVFrameSideDataType)(t)))
}

// Data returns the frame's plane base pointers exactly as FFmpeg stores them.
//
// Beware of planes with a negative linesize (bottom-up frames, as exported
// by some FFmpeg sinks): data[i] then points at the LAST row of the plane,
// and row y lives at data[i] + y*linesize[i]. The plane's memory footprint
// extends downwards from data[i], not upwards. Use Linesize to retrieve the
// signed strides before dereferencing these pointers.
func Data(f *Frame) (data [8]*uint8) {
	for i := range data {
		data[i] = (*uint8)(f.data[i])
	}
	return
}

// Linesize returns the frame's signed per-plane strides in bytes.
//
// A negative linesize[i] means plane i is stored bottom-up: data[i]
// addresses the last (bottom) row, and row y sits at
// data[i] + y*linesize[i], i.e. at decreasing addresses. The plane
// occupies rows*|linesize[i]| bytes ending just past data[i].
func Linesize(f *Frame) (linesize [8]int32) {
	for i := range linesize {
		linesize[i] = int32(f.linesize[i])
	}
	return
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// planeExtent computes the half-open address range [lo, hi) covered by
// plane i over the given number of rows, honoring negative (bottom-up)
// strides. ok is false when the plane is unusable (nil data pointer, zero
// stride, or non-positive row count).
func planeExtent(f *Frame, plane, rows int) (lo, hi uintptr, ok bool) {
	if rows <= 0 || f.data[plane] == nil || f.linesize[plane] == 0 {
		return 0, 0, false
	}
	stride := int(f.linesize[plane])
	base := uintptr(unsafe.Pointer(f.data[plane]))
	last := uintptr(int64(base) + int64(rows-1)*int64(stride))
	width := uintptr(absInt(stride))
	if last < base {
		return last, base + width, true
	}
	return base, last + width, true
}

// checkPlaneBounds verifies that the memory plane i spans over the given
// number of rows lies inside one of the frame's buffer references. Frames
// without buffer references (externally described memory) cannot be
// validated and are allowed through.
func checkPlaneBounds(f *Frame, plane, rows int) error {
	lo, hi, ok := planeExtent(f, plane, rows)
	if !ok {
		return fmt.Errorf("goav: plane %d is not readable (nil data, zero stride or no rows)", plane)
	}
	hasBuf := false
	inBuf := func(b *C.struct_AVBufferRef) bool {
		if b == nil {
			return false
		}
		hasBuf = true
		blo := uintptr(unsafe.Pointer(b.data))
		bhi := blo + uintptr(b.size)
		return lo >= blo && hi <= bhi
	}
	for i := 0; i < 8; i++ {
		if inBuf(f.buf[i]) {
			return nil
		}
	}
	if f.extended_buf != nil && f.nb_extended_buf > 0 {
		n := int(f.nb_extended_buf)
		eb := (*[1 << 16]*C.struct_AVBufferRef)(unsafe.Pointer(f.extended_buf))[:n:n]
		for _, b := range eb {
			if inBuf(b) {
				return nil
			}
		}
	}
	if !hasBuf {
		// No buffer references to validate against.
		return nil
	}
	return fmt.Errorf("goav: plane %d range [%#x, %#x) is outside the frame's buffers", plane, lo, hi)
}

// copyPlane copies rows of frame plane i into dst, which is laid out with
// dstStride bytes per row. Negative frame strides (bottom-up planes, where
// data[i] addresses the last row) are honored, so dst always ends up
// top-down. Only rowBytes per row are copied: stride padding never leaks
// into dst, and rows are never read past the plane's extent.
func copyPlane(dst []byte, dstStride int, f *Frame, plane, rowBytes, rows int) error {
	if rows <= 0 {
		return nil
	}
	if dstStride <= 0 || rows*dstStride > len(dst) {
		return fmt.Errorf("goav: destination too small for plane %d: need %d bytes, have %d", plane, rows*dstStride, len(dst))
	}
	if err := checkPlaneBounds(f, plane, rows); err != nil {
		return err
	}
	stride := int(f.linesize[plane])
	if a := absInt(stride); a < rowBytes {
		// Never read past the end of a source row.
		rowBytes = a
	}
	if rowBytes > dstStride {
		rowBytes = dstStride
	}
	p := uintptr(unsafe.Pointer(f.data[plane]))
	for y := 0; y < rows; y++ {
		row := C.GoBytes(unsafe.Pointer(p), C.int(rowBytes))
		copy(dst[y*dstStride:(y+1)*dstStride], row)
		p = uintptr(int64(p) + int64(stride))
	}
	return nil
}

// GetPicture creates a YCbCr image from the frame.
//
// The frame's width/height (not the padded linesize) define the picture
// size. Rows are copied one at a time, so stride padding is stripped, and
// negative strides (bottom-up frames) are flipped back to top-down order.
func GetPicture(f *Frame) (img *image.YCbCr, err error) {
	// For 4:4:4, CStride == YStride/1 && len(Cb) == len(Cr) == len(Y)/1.
	// For 4:2:2, CStride == YStride/2 && len(Cb) == len(Cr) == len(Y)/2.
	// For 4:2:0, CStride == YStride/2 && len(Cb) == len(Cr) == len(Y)/4.
	// For 4:4:0, CStride == YStride/1 && len(Cb) == len(Cr) == len(Y)/2.
	// For 4:1:1, CStride == YStride/4 && len(Cb) == len(Cr) == len(Y)/4.
	// For 4:1:0, CStride == YStride/4 && len(Cb) == len(Cr) == len(Y)/8.

	w := int(f.width)
	h := int(f.height)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("goav: invalid frame size %dx%d", w, h)
	}
	r := image.Rectangle{image.Point{0, 0}, image.Point{w, h}}
	// TODO: Use the sub sample ratio from the input image 'f.format'
	img = image.NewYCbCr(r, image.YCbCrSubsampleRatio420)
	if err = copyPlane(img.Y, img.YStride, f, 0, w, h); err != nil {
		return nil, err
	}
	// 4:2:0 chroma planes, rounded up for odd widths/heights.
	cw, ch := (w+1)/2, (h+1)/2
	if f.data[1] != nil {
		if err = copyPlane(img.Cb, img.CStride, f, 1, cw, ch); err != nil {
			return nil, err
		}
	}
	if f.data[2] != nil {
		if err = copyPlane(img.Cr, img.CStride, f, 2, cw, ch); err != nil {
			return nil, err
		}
	}
	return img, nil
}

// SetPicture sets the image pointer of |f| to the image pointers of |img|
func SetPicture(f *Frame, img *image.YCbCr) {
	d := Data(f)
	// l := Linesize(f)
	// FIXME: Save the original pointers somewhere, this is a memory leak
	d[0] = (*uint8)(unsafe.Pointer(&img.Y[0]))
	// d[1] = (*uint8)(unsafe.Pointer(&img.Cb[0]))
}

// GetPictureRGB creates an RGBA image from a frame holding packed
// 4-byte-per-pixel data (e.g. RGBA/BGRA) in plane 0.
//
// As with GetPicture, the frame's width/height define the picture size,
// stride padding is stripped row by row, and negative strides (bottom-up
// frames) are flipped back to top-down order.
func GetPictureRGB(f *Frame) (img *image.RGBA, err error) {
	w := int(f.width)
	h := int(f.height)
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("goav: invalid frame size %dx%d", w, h)
	}
	if stride := int(f.linesize[0]); absInt(stride) < 4*w {
		return nil, fmt.Errorf("goav: linesize %d too small for %d packed RGBA pixels (%d bytes/row)", stride, w, 4*w)
	}
	r := image.Rectangle{image.Point{0, 0}, image.Point{w, h}}
	img = image.NewRGBA(r)
	if err = copyPlane(img.Pix, img.Stride, f, 0, 4*w, h); err != nil {
		return nil, err
	}
	return img, nil
}

func AvSetFrame(f *Frame, w int, h int, pixFmt int) (err error) {
	f.width = C.int(w)
	f.height = C.int(h)
	f.format = C.int(pixFmt)
	if ret := C.av_frame_get_buffer((*C.struct_AVFrame)(unsafe.Pointer(f)), 32 /*alignment*/); ret < 0 {
		err = fmt.Errorf("Error allocating avframe buffer. Err: %v", ret)
		return
	}
	return
}

func AvFrameGetInfo(f *Frame) (width int, height int, linesize [8]int32, data [8]*uint8) {
	width = int(f.linesize[0])
	height = int(f.height)
	for i := range linesize {
		linesize[i] = int32(f.linesize[i])
	}
	for i := range data {
		data[i] = (*uint8)(f.data[i])
	}
	// log.Println("Linesize is ", f.linesize, "Data is", data)
	return
}

func GetBestEffortTimestamp(f *Frame) int64 {
	return int64(f.best_effort_timestamp)
}

// //static int get_video_buffer (Frame *frame, int align)
// func GetVideoBuffer(f *Frame, a int) int {
// 	return int(C.get_video_buffer(f, C.int(a)))
// }

// //static int get_audio_buffer (Frame *frame, int align)
// func GetAudioBuffer(f *Frame, a int) int {
// 	return C.get_audio_buffer(f, C.int(a))
// }

// //static void get_frame_defaults (Frame *frame)
// func GetFrameDefaults(f *Frame) {
// 	C.get_frame_defaults(*C.struct_AVFrame(f))
// }
