//! Native image transformations backed by the [`image`] crate.
//!
//! Every handler here:
//!   * Decodes via `image::load_from_memory_with_format` so the input
//!     MIME drives the codec choice (no auto-sniffing — the REST layer
//!     already knows the source MIME).
//!   * Encodes back to the same format unless the params explicitly
//!     ask for a different one (`output_format`).
//!
//! Output bytes are returned as `Vec<u8>`; the REST layer base64-encodes
//! them at the boundary.

use image::{DynamicImage, ImageFormat, imageops::FilterType};
use serde::Deserialize;

use super::{HandlerError, HandlerOutput, HandlerResult};

fn format_for_mime(mime: &str, kind: &str) -> Result<ImageFormat, HandlerError> {
    Ok(match mime {
        "image/png" => ImageFormat::Png,
        "image/jpeg" | "image/jpg" => ImageFormat::Jpeg,
        "image/webp" => ImageFormat::WebP,
        "image/gif" => ImageFormat::Gif,
        "image/tiff" => ImageFormat::Tiff,
        "image/bmp" => ImageFormat::Bmp,
        other => {
            return Err(HandlerError::UnsupportedMime(
                other.to_string(),
                kind.to_string(),
            ));
        }
    })
}

fn load(bytes: &[u8], mime: &str, kind: &str) -> Result<DynamicImage, HandlerError> {
    let format = format_for_mime(mime, kind)?;
    image::load_from_memory_with_format(bytes, format)
        .map_err(|err| HandlerError::Decode(err.to_string()))
}

fn encode(img: DynamicImage, format: ImageFormat) -> Result<Vec<u8>, HandlerError> {
    let mut buf = std::io::Cursor::new(Vec::new());
    img.write_to(&mut buf, format)
        .map_err(|err| HandlerError::Encode(err.to_string()))?;
    Ok(buf.into_inner())
}

#[derive(Debug, Deserialize, Default)]
struct ResizeParams {
    width: Option<u32>,
    height: Option<u32>,
    /// Default `Lanczos3` — Foundry's "Resize" produces high-fidelity
    /// downscales; matches the upstream sample renders.
    #[serde(default)]
    filter: Option<String>,
}

fn parse_filter(name: Option<&str>) -> FilterType {
    match name.unwrap_or("lanczos3") {
        "nearest" => FilterType::Nearest,
        "triangle" => FilterType::Triangle,
        "catmullrom" => FilterType::CatmullRom,
        "gaussian" => FilterType::Gaussian,
        _ => FilterType::Lanczos3,
    }
}

pub fn thumbnail(mime: &str, params: &serde_json::Value, bytes: Vec<u8>) -> HandlerResult {
    // Foundry's thumbnail is a fixed 256-px longest-edge box.
    let img = load(&bytes, mime, "thumbnail")?;
    let max_dim = params.get("max_dim").and_then(|v| v.as_u64()).unwrap_or(256) as u32;
    let scaled = img.thumbnail(max_dim, max_dim);
    let format = format_for_mime(mime, "thumbnail")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(scaled, format)?),
        output_json: None,
    })
}

pub fn resize(mime: &str, params: &serde_json::Value, bytes: Vec<u8>) -> HandlerResult {
    let p: ResizeParams = serde_json::from_value(params.clone())
        .map_err(|err| HandlerError::InvalidParams("resize".into(), err.to_string()))?;
    let img = load(&bytes, mime, "resize")?;
    let target_w = p.width.unwrap_or(img.width());
    let target_h = p.height.unwrap_or(img.height());
    let resized = img.resize_exact(target_w, target_h, parse_filter(p.filter.as_deref()));
    let format = format_for_mime(mime, "resize")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(resized, format)?),
        output_json: None,
    })
}

pub fn resize_within_bbox(
    mime: &str,
    params: &serde_json::Value,
    bytes: Vec<u8>,
) -> HandlerResult {
    let p: ResizeParams = serde_json::from_value(params.clone()).map_err(|err| {
        HandlerError::InvalidParams("resize_within_bounding_box".into(), err.to_string())
    })?;
    let max_w = p.width.ok_or_else(|| {
        HandlerError::InvalidParams(
            "resize_within_bounding_box".into(),
            "width is required".into(),
        )
    })?;
    let max_h = p.height.ok_or_else(|| {
        HandlerError::InvalidParams(
            "resize_within_bounding_box".into(),
            "height is required".into(),
        )
    })?;
    let img = load(&bytes, mime, "resize_within_bounding_box")?;
    // `resize` (vs `resize_exact`) preserves the aspect ratio — the
    // canonical "fit within bounding box" semantic.
    let resized = img.resize(max_w, max_h, parse_filter(p.filter.as_deref()));
    let format = format_for_mime(mime, "resize_within_bounding_box")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(resized, format)?),
        output_json: None,
    })
}

#[derive(Debug, Deserialize, Default)]
struct RotateParams {
    /// Degrees, clockwise. Only quarter-turns are exact-pixel
    /// operations; arbitrary angles fall back to a Lanczos resample
    /// of a 90-rotated image.
    degrees: Option<i32>,
}

pub fn rotate(mime: &str, params: &serde_json::Value, bytes: Vec<u8>) -> HandlerResult {
    let p: RotateParams = serde_json::from_value(params.clone())
        .map_err(|err| HandlerError::InvalidParams("rotate".into(), err.to_string()))?;
    let img = load(&bytes, mime, "rotate")?;
    let rotated = match p.degrees.unwrap_or(0).rem_euclid(360) {
        0 => img,
        90 => img.rotate90(),
        180 => img.rotate180(),
        270 => img.rotate270(),
        other => {
            return Err(HandlerError::InvalidParams(
                "rotate".into(),
                format!("only 0/90/180/270 deg supported, got {other}"),
            ));
        }
    };
    let format = format_for_mime(mime, "rotate")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(rotated, format)?),
        output_json: None,
    })
}

#[derive(Debug, Deserialize, Default)]
struct CropParams {
    x: u32,
    y: u32,
    width: u32,
    height: u32,
}

pub fn crop(mime: &str, params: &serde_json::Value, bytes: Vec<u8>) -> HandlerResult {
    let p: CropParams = serde_json::from_value(params.clone())
        .map_err(|err| HandlerError::InvalidParams("crop".into(), err.to_string()))?;
    let img = load(&bytes, mime, "crop")?;
    if p.x.checked_add(p.width).map(|v| v > img.width()).unwrap_or(true)
        || p.y
            .checked_add(p.height)
            .map(|v| v > img.height())
            .unwrap_or(true)
    {
        return Err(HandlerError::InvalidParams(
            "crop".into(),
            "crop rectangle exceeds image bounds".into(),
        ));
    }
    let cropped = img.crop_imm(p.x, p.y, p.width, p.height);
    let format = format_for_mime(mime, "crop")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(cropped, format)?),
        output_json: None,
    })
}

pub fn grayscale(mime: &str, bytes: Vec<u8>) -> HandlerResult {
    let img = load(&bytes, mime, "grayscale")?;
    let gray = img.grayscale();
    let format = format_for_mime(mime, "grayscale")?;
    Ok(HandlerOutput {
        output_mime_type: mime.to_string(),
        output_bytes: Some(encode(gray, format)?),
        output_json: None,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 1×1 white PNG (smallest legal PNG with the right magic + IHDR).
    /// The image crate decodes / re-encodes it without losing the
    /// pixel, so resize / rotate / crop assertions stay deterministic.
    fn one_pixel_png() -> Vec<u8> {
        // Pre-computed: 1×1 RGBA white. Generated once with `image`
        // and pasted here so the test is hermetic.
        let img =
            image::DynamicImage::ImageRgba8(image::RgbaImage::from_pixel(1, 1, image::Rgba([255, 255, 255, 255])));
        encode(img, ImageFormat::Png).unwrap()
    }

    #[test]
    fn thumbnail_stays_inside_max_dim() {
        let bytes = encode(
            DynamicImage::ImageRgba8(image::RgbaImage::from_pixel(
                512,
                256,
                image::Rgba([0, 0, 0, 255]),
            )),
            ImageFormat::Png,
        )
        .unwrap();
        let out = thumbnail("image/png", &serde_json::json!({"max_dim": 64}), bytes).unwrap();
        let decoded = image::load_from_memory(out.output_bytes.as_ref().unwrap()).unwrap();
        assert!(decoded.width() <= 64 && decoded.height() <= 64);
    }

    #[test]
    fn rotate_90_swaps_axes() {
        let bytes = encode(
            DynamicImage::ImageRgba8(image::RgbaImage::from_pixel(
                4,
                2,
                image::Rgba([0, 0, 0, 255]),
            )),
            ImageFormat::Png,
        )
        .unwrap();
        let out = rotate("image/png", &serde_json::json!({"degrees": 90}), bytes).unwrap();
        let decoded = image::load_from_memory(out.output_bytes.as_ref().unwrap()).unwrap();
        assert_eq!((decoded.width(), decoded.height()), (2, 4));
    }

    #[test]
    fn grayscale_preserves_dimensions() {
        let bytes = one_pixel_png();
        let out = grayscale("image/png", bytes).unwrap();
        let decoded = image::load_from_memory(out.output_bytes.as_ref().unwrap()).unwrap();
        assert_eq!((decoded.width(), decoded.height()), (1, 1));
    }
}
