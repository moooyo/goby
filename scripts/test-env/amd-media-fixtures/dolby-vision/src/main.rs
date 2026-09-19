use std::error::Error;
use std::fs::{self, File};
use std::io::{BufWriter, Write};
use std::path::Path;

use dolby_vision::rpu::dovi_rpu::DoviRpu;
use dolby_vision::rpu::generate::GenerateConfig;
use dolby_vision::rpu::rpu_data_nlq::DoviELType;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 3 {
        return Err("Usage: goby-synthetic-dovi-rpu CONFIG_JSON OUTPUT_DIRECTORY".into());
    }
    let config: GenerateConfig = serde_json::from_slice(&fs::read(&args[1])?)?;
    if !(2..=240).contains(&config.length) {
        return Err("The fixture must contain 2 to 240 frames at 24 fps".into());
    }
    let directory = Path::new(&args[2]);
    fs::create_dir_all(directory)?;

    let identity = DoviRpu::profile81_config(&config)?;
    let mut reshaped = identity.clone();
    let polynomial = reshaped
        .rpu_data_mapping
        .as_mut()
        .ok_or("Missing mapping")?
        .curves[0]
        .polynomial
        .as_mut()
        .ok_or("Missing luma polynomial")?;
    // The default coefficient denominator is 2^23. Keep both chroma curves
    // unchanged and deliberately change luma to f(x) = 1/16 + 3*x/4.
    polynomial.poly_coef_int[0][0] = 0;
    polynomial.poly_coef_int[0][1] = 0;
    polynomial.poly_coef[0][0] = 1 << 19;
    polynomial.poly_coef[0][1] = 3 << 21;
    reshaped.modified = true;

    // These two variants are metadata parser fixtures, not complete P7 media.
    // Real MEL has disable_residual_flag=false and zero residual NLQ, while
    // FEL can require an enhancement layer. Never infer MEL from that flag alone.
    let mut mel = reshaped.clone();
    mel.convert_with_mode(1_u8)?;
    if mel.dovi_profile != 7 || mel.get_enhancement_layer_type() != Some(DoviELType::MEL) {
        return Err("The pinned library did not produce profile 7 MEL metadata".into());
    }
    let mut residual = mel.clone();
    residual
        .rpu_data_mapping
        .as_mut()
        .and_then(|mapping| mapping.nlq.as_mut())
        .ok_or("Missing NLQ")?
        .linear_deadzone_slope[0] = 1 << 22;
    residual.modified = true;
    residual.el_type = residual.get_enhancement_layer_type();
    if residual.el_type != Some(DoviELType::FEL) {
        return Err("The negative metadata must require nonzero residual processing".into());
    }

    for (name, template) in [
        ("profile81-identity", identity),
        ("profile81-nonidentity", reshaped),
        ("profile7-mel-metadata-only", mel),
        ("profile7-residual-metadata-only", residual),
    ] {
        let mut file = BufWriter::new(File::create(directory.join(format!("{name}-rpu.bin")))?);
        for frame in 0..config.length {
            let mut rpu = template.clone();
            rpu.vdr_dm_data.as_mut().ok_or("Missing DM data")?.set_scene_cut(frame == 0);
            rpu.modified = true;
            let encoded = rpu.write_hevc_unspec62_nalu()?;
            // Parse every generated RPU, including its CRC, before publishing it.
            let decoded = DoviRpu::parse_unspec62_nalu(&encoded)?;
            if decoded.dovi_profile != rpu.dovi_profile {
                return Err("RPU profile changed during serialization".into());
            }
            if frame == 0 {
                fs::write(
                    directory.join(format!("{name}-first-rpu.json")),
                    serde_json::to_vec_pretty(&decoded)?,
                )?;
            }
            // Use the dovi_tool RPU.bin convention: start code + RPU payload,
            // without the two-byte HEVC UNSPEC62 NAL header.
            file.write_all(&[0, 0, 0, 1])?;
            file.write_all(&encoded[2..])?;
        }
        file.flush()?;
    }
    Ok(())
}
