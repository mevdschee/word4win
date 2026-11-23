package doc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	_ "image/gif" // pictures inserted from GIF files
	"image/png"
	"io"
)

func encodePNG(w io.Writer, img image.Image) error {
	return (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(w, img)
}

// decodeDIB decodes a device independent bitmap: a BITMAPINFOHEADER (or a
// later version of it), the colour table and the pixels. It supports 1, 4,
// 8, 16, 24 and 32 bits per pixel, uncompressed or with bit fields.
func decodeDIB(d []byte) (image.Image, error) {
	if len(d) < 40 {
		return nil, errors.New("dib: short header")
	}
	hdr := int(le32(d))
	if hdr < 40 || hdr > len(d) {
		return nil, errors.New("dib: bad header")
	}
	w := int(int32(le32(d[4:])))
	h := int(int32(le32(d[8:])))
	bpp := int(le16(d[14:]))
	comp := le32(d[16:])
	clrUsed := int(le32(d[32:]))
	topDown := h < 0
	if topDown {
		h = -h
	}
	if w <= 0 || h <= 0 || w > 1<<15 || h > 1<<15 {
		return nil, errors.New("dib: bad size")
	}
	if comp != 0 && comp != 3 { // BI_RGB, BI_BITFIELDS
		return nil, errors.New("dib: compressed")
	}
	pos := hdr
	masks := [3]uint32{0x7C00, 0x03E0, 0x001F}
	if bpp == 32 {
		masks = [3]uint32{0xFF0000, 0xFF00, 0xFF}
	}
	if comp == 3 {
		if hdr == 40 { // the masks follow the header
			if len(d) < pos+12 {
				return nil, errors.New("dib: short masks")
			}
			masks = [3]uint32{le32(d[pos:]), le32(d[pos+4:]), le32(d[pos+8:])}
			pos += 12
		} else {
			masks = [3]uint32{le32(d[40:]), le32(d[44:]), le32(d[48:])}
		}
	}
	var palette []color.RGBA
	if bpp <= 8 {
		n := clrUsed
		if n == 0 {
			n = 1 << bpp
		}
		if len(d) < pos+4*n {
			return nil, errors.New("dib: short palette")
		}
		for i := 0; i < n; i++ {
			palette = append(palette, color.RGBA{d[pos+2], d[pos+1], d[pos], 0xFF})
			pos += 4
		}
	}
	stride := (w*bpp + 31) / 32 * 4
	if len(d) < pos+stride*h {
		return nil, errors.New("dib: short pixels")
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	alpha := false
	for y := 0; y < h; y++ {
		row := d[pos+y*stride : pos+(y+1)*stride]
		dy := h - 1 - y
		if topDown {
			dy = y
		}
		for x := 0; x < w; x++ {
			var c color.RGBA
			switch bpp {
			case 1, 4, 8:
				bit := x * bpp
				idx := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if idx < len(palette) {
					c = palette[idx]
				}
			case 16:
				v := uint32(le16(row[x*2:]))
				c = color.RGBA{field(v, masks[0]), field(v, masks[1]), field(v, masks[2]), 0xFF}
			case 24:
				c = color.RGBA{row[x*3+2], row[x*3+1], row[x*3], 0xFF}
			case 32:
				v := le32(row[x*4:])
				c = color.RGBA{field(v, masks[0]), field(v, masks[1]), field(v, masks[2]), row[x*4+3]}
				if c.A != 0 {
					alpha = true
				}
			default:
				return nil, errors.New("dib: unsupported depth")
			}
			img.SetRGBA(x, dy, c)
		}
	}
	if bpp == 32 && !alpha { // no alpha channel in use: opaque
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xFF
		}
	} else if bpp == 32 {
		// Straight alpha to the premultiplied colours of image.RGBA.
		for i := 0; i < len(img.Pix); i += 4 {
			a := uint32(img.Pix[i+3])
			for k := 0; k < 3; k++ {
				img.Pix[i+k] = uint8(uint32(img.Pix[i+k]) * a / 255)
			}
		}
	}
	return img, nil
}

// field extracts a colour channel with a bit mask, scaled to 8 bits.
func field(v, mask uint32) uint8 {
	if mask == 0 {
		return 0
	}
	shift := 0
	for mask&1 == 0 {
		mask >>= 1
		shift++
	}
	bits := 0
	for m := mask; m&1 == 1; m >>= 1 {
		bits++
	}
	x := (v >> shift) & mask
	if bits >= 8 {
		return uint8(x >> (bits - 8))
	}
	return uint8(x * 255 / mask)
}

// LoadImage reads a picture file for inserting: PNG and JPEG are kept as
// they are, GIF and BMP are converted to PNG. The size is the pixel size
// at 96 dpi.
func LoadImage(data []byte) (*Image, error) {
	if len(data) > 2 && data[0] == 'B' && data[1] == 'M' && len(data) > 14 {
		img, err := decodeDIB(data[14:])
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := encodePNG(&b, img); err != nil {
			return nil, err
		}
		data = b.Bytes()
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	switch format {
	case "png", "jpeg":
	default:
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		var b bytes.Buffer
		if err := encodePNG(&b, img); err != nil {
			return nil, err
		}
		data, format = b.Bytes(), "png"
	}
	return &Image{Data: data, Format: format, Width: float64(cfg.Width) * 0.75, Height: float64(cfg.Height) * 0.75}, nil
}
