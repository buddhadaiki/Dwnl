"""Writes the installer's images from the Dwnl logo.

Run from the project root with Pillow installed:  python tools/installer-art.py
The output in installer/assets is committed; run this again only when the logo changes.
"""
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = Path(__file__).resolve().parent.parent
LOGO = ROOT / "docs/branding/dwnl-logo.png"
OUT = ROOT / "installer/assets"
FONT = Path("C:/Windows/Fonts/seguisb.ttf")  # Segoe UI Semibold, as the app uses

# Inno Setup 7 image areas at 100%-250% scaling with default fonts.
SMALL_SIZES = [58, 77, 97, 116, 124, 143, 159]
SIDE_SIZES = [(202, 386), (269, 515), (336, 643), (403, 772), (430, 824), (498, 953), (534, 1022)]
ICON_SIZES = [16, 20, 24, 32, 40, 48, 64, 256]
# The app's royal blue accent: --accent and --accent-press in frontend/style.css.
PANELS = {"light": ((0x4F, 0x76, 0xEA), (0x41, 0x69, 0xE1)), "dark": ((0x3A, 0x5F, 0xD2), (0x2A, 0x46, 0xA0))}


def mark() -> Image.Image:
    """The logo cropped to its shape, without the stray translucent specks around it."""
    logo = Image.open(LOGO).convert("RGBA")
    alpha = logo.getchannel("A").point(lambda a: 255 if a >= 128 else 0)
    alpha = alpha.filter(ImageFilter.MinFilter(5)).filter(ImageFilter.MaxFilter(5))
    logo.putalpha(alpha)
    return logo.crop(alpha.getbbox())


def square(image: Image.Image, margin: float) -> Image.Image:
    side = round(max(image.size) * (1 + 2 * margin))
    canvas = Image.new("RGBA", (side, side), (0, 0, 0, 0))
    canvas.paste(image, ((side - image.width) // 2, (side - image.height) // 2), image)
    return canvas


def side_panel(shape: Image.Image, top: tuple, bottom: tuple) -> Image.Image:
    width, height = 1068, 2044  # twice the largest area, shrunk to each size
    panel = Image.new("RGBA", (width, height))
    draw = ImageDraw.Draw(panel)
    for y in range(height):
        t = y / (height - 1)
        draw.line([(0, y), (width, y)], fill=tuple(round(a + (b - a) * t) for a, b in zip(top, bottom)) + (255,))
    white = Image.new("RGBA", shape.size, (255, 255, 255, 255))
    white.putalpha(shape.getchannel("A"))
    logo_width = round(width * 0.46)
    white = white.resize((logo_width, round(shape.height * logo_width / shape.width)), Image.Resampling.LANCZOS)
    logo_top = round(height * 0.34) - white.height // 2
    panel.alpha_composite(white, ((width - white.width) // 2, logo_top))
    font = ImageFont.truetype(str(FONT), round(width * 0.17))
    text = "Dwnl"
    left, text_top, right, text_bottom = draw.textbbox((0, 0), text, font=font)
    draw.text(((width - (right - left)) // 2 - left, logo_top + white.height + round(height * 0.035) - text_top), text, font=font, fill=(255, 255, 255, 255))
    return panel


def main() -> None:
    OUT.mkdir(parents=True, exist_ok=True)
    shape = mark()
    small = square(shape, 0.04)
    for size in SMALL_SIZES:
        small.resize((size, size), Image.Resampling.LANCZOS).save(OUT / f"dwnl-small-{size}.png", optimize=True)
    for mode, (top, bottom) in PANELS.items():
        panel = side_panel(shape, top, bottom)
        for width, height in SIDE_SIZES:
            panel.resize((width, height), Image.Resampling.LANCZOS).convert("RGB").save(OUT / f"dwnl-side-{mode}-{width}.png", optimize=True)
    icon = square(shape, 0.06).resize((256, 256), Image.Resampling.LANCZOS)
    icon.save(OUT / "dwnl.ico", format="ICO", sizes=[(s, s) for s in ICON_SIZES])


if __name__ == "__main__":
    main()
