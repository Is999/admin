package admin

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"github.com/Is999/go-utils/errors"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	base64Captcha "github.com/mojocn/base64Captcha"
	"golang.org/x/image/font"
)

const (
	// loginCaptchaImageHeight 表示登录图形验证码图片高度。
	loginCaptchaImageHeight = 44
	// loginCaptchaImageWidth 表示登录图形验证码图片宽度。
	loginCaptchaImageWidth = 120
	// loginCaptchaFontSize 表示登录图形验证码字体大小。
	loginCaptchaFontSize = 31
	// loginCaptchaPaddingX 表示验证码左右安全留白，避免字符贴边裁切。
	loginCaptchaPaddingX = 6
	// loginCaptchaGuideLineCount 表示背景淡线数量，先于文字绘制，避免遮挡字符。
	loginCaptchaGuideLineCount = 3
	// loginCaptchaNoiseMarkCount 表示背景图案噪声数量，避免验证码过于干净。
	loginCaptchaNoiseMarkCount = 7
	// loginCaptchaDPI 表示验证码字体渲染 DPI。
	loginCaptchaDPI = 72
	// loginCaptchaMimeType 表示验证码图片 MIME 类型。
	loginCaptchaMimeType = "image/png"
	// loginCaptchaRandomBytes 表示单张图片预读的随机字节数。
	loginCaptchaRandomBytes = 64
)

// captchaImageRandom 保存单张图片的随机数据，避免每个噪声点单独读取系统随机源。
type captchaImageRandom struct {
	values [loginCaptchaRandomBytes]byte // values 保存本张图片使用的随机字节
	index  int                           // index 表示下一个待读取位置
}

// loginCaptchaFont 使用稳定字体渲染验证码，避免随机花体导致字符裁切或难辨认。
var loginCaptchaFont = base64Captcha.DefaultEmbeddedFonts.LoadFontByName("fonts/wqy-microhei.ttc")

// loginCaptchaBackgroundColors 定义登录验证码的浅色背景池。
var loginCaptchaBackgroundColors = []color.RGBA{
	{R: 244, G: 240, B: 255, A: 255},
	{R: 239, G: 246, B: 255, A: 255},
	{R: 255, G: 242, B: 246, A: 255},
	{R: 240, G: 253, B: 244, A: 255},
}

// loginCaptchaGuideLineColors 定义登录验证码的淡背景线颜色池。
var loginCaptchaGuideLineColors = []color.RGBA{
	{R: 184, G: 203, B: 226, A: 255},
	{R: 203, G: 190, B: 229, A: 255},
	{R: 219, G: 191, B: 209, A: 255},
	{R: 188, G: 215, B: 198, A: 255},
}

// loginCaptchaNoiseMarkColors 定义登录验证码图案噪声颜色池。
var loginCaptchaNoiseMarkColors = []color.RGBA{
	{R: 198, G: 211, B: 234, A: 255},
	{R: 214, G: 198, B: 238, A: 255},
	{R: 232, G: 202, B: 221, A: 255},
	{R: 197, G: 226, B: 209, A: 255},
	{R: 236, G: 219, B: 162, A: 255},
	{R: 203, G: 226, B: 230, A: 255},
	{R: 230, G: 207, B: 186, A: 255},
	{R: 210, G: 222, B: 187, A: 255},
}

// loginCaptchaTextColors 定义登录验证码文字颜色池，保证深色文字覆盖淡背景线。
var loginCaptchaTextColors = []color.RGBA{
	{R: 37, G: 99, B: 235, A: 255},
	{R: 124, G: 58, B: 237, A: 255},
	{R: 220, G: 38, B: 38, A: 255},
	{R: 5, G: 150, B: 105, A: 255},
	{R: 217, G: 119, B: 6, A: 255},
	{R: 8, G: 145, B: 178, A: 255},
	{R: 225, G: 29, B: 72, A: 255},
	{R: 79, G: 70, B: 229, A: 255},
}

// buildLoginCaptchaImageDataURL 把验证码文本渲染成 PNG data URL。
func buildLoginCaptchaImageDataURL(code string) (string, error) {
	imageBytes, err := drawLoginCaptchaPNG(code)
	if err != nil {
		return "", errors.Tag(err)
	}
	return fmt.Sprintf(
		"data:%s;base64,%s",
		loginCaptchaMimeType,
		base64.StdEncoding.EncodeToString(imageBytes),
	), nil
}

// drawLoginCaptchaPNG 渲染验证码 PNG；干扰线先画、文字后画，避免线条遮挡字符。
func drawLoginCaptchaPNG(code string) ([]byte, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, errors.New("验证码内容不能为空")
	}
	random := &captchaImageRandom{}
	if _, err := rand.Read(random.values[:]); err != nil {
		return nil, errors.Wrap(err, "读取登录验证码图片随机数据失败")
	}
	background := random.color(loginCaptchaBackgroundColors)
	canvas := image.NewNRGBA(image.Rect(0, 0, loginCaptchaImageWidth, loginCaptchaImageHeight))
	draw.Draw(canvas, canvas.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	drawLoginCaptchaGuideLines(canvas, random)
	drawLoginCaptchaNoiseMarks(canvas, random)
	if err := drawLoginCaptchaText(canvas, code, random); err != nil {
		return nil, errors.Tag(err)
	}
	buffer := bytes.Buffer{}
	if err := png.Encode(&buffer, canvas); err != nil {
		return nil, errors.Wrap(err, "编码登录验证码 PNG 失败")
	}
	return buffer.Bytes(), nil
}

// color 从指定颜色池选择一个颜色。
func (r *captchaImageRandom) color(colors []color.RGBA) color.RGBA {
	return colors[r.intn(len(colors))]
}

// intn 返回 [0, max) 范围内的图片随机数。
func (r *captchaImageRandom) intn(max int) int {
	if max <= 0 {
		return 0
	}
	value := r.values[r.index%len(r.values)]
	r.index++
	return int(value) % max
}

// offset 返回 [-limit, limit] 范围内的图片随机偏移。
func (r *captchaImageRandom) offset(limit int) int {
	return r.intn(limit*2+1) - limit
}

// drawLoginCaptchaGuideLines 绘制淡背景线，提供轻量干扰但不覆盖最终文字。
func drawLoginCaptchaGuideLines(canvas *image.NRGBA, random *captchaImageRandom) {
	for range loginCaptchaGuideLineCount {
		lineColor := random.color(loginCaptchaGuideLineColors)
		startOffset := random.intn(loginCaptchaImageHeight - 16)
		endOffset := random.intn(loginCaptchaImageHeight - 16)
		drawLoginCaptchaLine(canvas, 4, 8+startOffset, loginCaptchaImageWidth-5, 8+endOffset, lineColor)
	}
}

// drawLoginCaptchaNoiseMarks 绘制少量雪花、五星和圆点，增加背景噪声但不覆盖文字。
func drawLoginCaptchaNoiseMarks(canvas *image.NRGBA, random *captchaImageRandom) {
	for range loginCaptchaNoiseMarkCount {
		markColor := random.color(loginCaptchaNoiseMarkColors)
		centerX := loginCaptchaPaddingX + random.intn(loginCaptchaImageWidth-loginCaptchaPaddingX*2)
		centerY := 6 + random.intn(loginCaptchaImageHeight-12)
		radius := random.intn(3)
		switch random.intn(3) {
		case 0:
			drawLoginCaptchaSnowflake(canvas, centerX, centerY, radius+3, markColor)
		case 1:
			drawLoginCaptchaStar(canvas, centerX, centerY, radius+3, markColor)
		default:
			drawLoginCaptchaDot(canvas, centerX, centerY, radius+1, markColor)
		}
	}
}

// drawLoginCaptchaSnowflake 绘制轻量雪花噪声。
func drawLoginCaptchaSnowflake(canvas *image.NRGBA, centerX int, centerY int, radius int, markColor color.RGBA) {
	drawLoginCaptchaLine(canvas, centerX-radius, centerY, centerX+radius, centerY, markColor)
	drawLoginCaptchaLine(canvas, centerX, centerY-radius, centerX, centerY+radius, markColor)
	drawLoginCaptchaLine(canvas, centerX-radius+1, centerY-radius+1, centerX+radius-1, centerY+radius-1, markColor)
	drawLoginCaptchaLine(canvas, centerX-radius+1, centerY+radius-1, centerX+radius-1, centerY-radius+1, markColor)
}

// drawLoginCaptchaStar 绘制轻量五星噪声。
func drawLoginCaptchaStar(canvas *image.NRGBA, centerX int, centerY int, radius int, markColor color.RGBA) {
	drawLoginCaptchaLine(canvas, centerX, centerY-radius, centerX+radius, centerY+radius-1, markColor)
	drawLoginCaptchaLine(canvas, centerX+radius, centerY+radius-1, centerX-radius, centerY-1, markColor)
	drawLoginCaptchaLine(canvas, centerX-radius, centerY-1, centerX+radius, centerY-1, markColor)
	drawLoginCaptchaLine(canvas, centerX+radius, centerY-1, centerX-radius, centerY+radius-1, markColor)
	drawLoginCaptchaLine(canvas, centerX-radius, centerY+radius-1, centerX, centerY-radius, markColor)
}

// drawLoginCaptchaDot 绘制小圆点噪声。
func drawLoginCaptchaDot(canvas *image.NRGBA, centerX int, centerY int, radius int, markColor color.RGBA) {
	for y := centerY - radius; y <= centerY+radius; y++ {
		for x := centerX - radius; x <= centerX+radius; x++ {
			if (x-centerX)*(x-centerX)+(y-centerY)*(y-centerY) <= radius*radius && image.Pt(x, y).In(canvas.Bounds()) {
				canvas.Set(x, y, markColor)
			}
		}
	}
}

// drawLoginCaptchaText 按字符槽位绘制文本，保证每个字符位于图片安全区域内。
func drawLoginCaptchaText(canvas *image.NRGBA, code string, random *captchaImageRandom) error {
	runes := []rune(code)
	if len(runes) == 0 {
		return errors.New("验证码内容不能为空")
	}
	context := freetype.NewContext()
	context.SetDPI(loginCaptchaDPI)
	context.SetClip(canvas.Bounds())
	context.SetDst(canvas)
	context.SetFont(loginCaptchaFont)
	context.SetFontSize(loginCaptchaFontSize)
	context.SetHinting(font.HintingFull)

	face := truetype.NewFace(loginCaptchaFont, &truetype.Options{
		DPI:     loginCaptchaDPI,
		Hinting: font.HintingFull,
		Size:    loginCaptchaFontSize,
	})
	defer face.Close()

	metrics := face.Metrics()
	ascent := metrics.Ascent.Ceil()
	descent := metrics.Descent.Ceil()
	baseline := (loginCaptchaImageHeight-ascent-descent)/2 + ascent
	cellWidth := (loginCaptchaImageWidth - loginCaptchaPaddingX*2) / len(runes)
	drawer := font.Drawer{Face: face}

	for index, char := range runes {
		textColor := random.color(loginCaptchaTextColors)
		offsetX := random.offset(1)
		offsetY := random.offset(2)
		text := string(char)
		advance := drawer.MeasureString(text).Ceil()
		x := loginCaptchaPaddingX + index*cellWidth + (cellWidth-advance)/2 + offsetX
		x = clampInt(x, loginCaptchaPaddingX/2, loginCaptchaImageWidth-loginCaptchaPaddingX-advance)
		y := clampInt(baseline+offsetY, ascent+1, loginCaptchaImageHeight-descent-2)
		context.SetSrc(image.NewUniform(textColor))
		if _, err := context.DrawString(text, freetype.Pt(x, y)); err != nil {
			return errors.Wrap(err, "绘制登录验证码文字失败")
		}
	}
	return nil
}

// drawLoginCaptchaLine 使用 Bresenham 算法绘制单像素线。
func drawLoginCaptchaLine(canvas *image.NRGBA, x0 int, y0 int, x1 int, y1 int, lineColor color.RGBA) {
	dx := absInt(x1 - x0)
	dy := -absInt(y1 - y0)
	stepX := -1
	if x0 < x1 {
		stepX = 1
	}
	stepY := -1
	if y0 < y1 {
		stepY = 1
	}
	err := dx + dy
	for {
		if image.Pt(x0, y0).In(canvas.Bounds()) {
			canvas.Set(x0, y0, lineColor)
		}
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := err * 2
		if e2 >= dy {
			err += dy
			x0 += stepX
		}
		if e2 <= dx {
			err += dx
			y0 += stepY
		}
	}
}

// clampInt 把整数限制到指定闭区间。
func clampInt(value int, minValue int, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

// absInt 返回整数绝对值。
func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}
