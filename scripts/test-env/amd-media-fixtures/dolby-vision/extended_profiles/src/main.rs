use std::error::Error;
use std::fs::{self, File};
use std::io::{BufWriter, Write};
use std::path::Path;

use dolby_vision::rpu::dovi_rpu::DoviRpu;
use dolby_vision::rpu::generate::GenerateConfig;

fn main() -> Result<(), Box<dyn Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 4 {
        return Err("Usage: goby-extended-dovi-rpu PROFILE CONFIG_JSON OUTPUT_DIRECTORY".into());
    }
    let config: GenerateConfig = serde_json::from_slice(&fs::read(&args[2])?)?;
    if !(48..=1440).contains(&config.length) {
        return Err("Expected 48 to 1440 frames".into());
    }
    let mut template = match args[1].as_str() {
        "5" => DoviRpu::profile5_config(&config)?,
        "8.4" => DoviRpu::profile84_config(&config)?,
        "8.2" => DoviRpu::profile81_config(&config)?,
        _ => return Err("Unsupported fixture profile".into()),
    };
    if args[1] == "8.2" {
        // The base pixels are original BT.709 SDR samples. Author a deliberate
        // analytic grade with BT.709 YCbCr decoding and BT.709-to-HPE conversion.
        // This is not a Profile 8.1 RPU or a PQ base relabeled as SDR.
        let dm = template.vdr_dm_data.as_mut().ok_or("Missing DM")?;
        dm.ycc_to_rgb_coef0 = 9576;
        dm.ycc_to_rgb_coef1 = 0;
        dm.ycc_to_rgb_coef2 = 14744;
        dm.ycc_to_rgb_coef3 = 9576;
        dm.ycc_to_rgb_coef4 = -1754;
        dm.ycc_to_rgb_coef5 = -4383;
        dm.ycc_to_rgb_coef6 = 9576;
        dm.ycc_to_rgb_coef7 = 17373;
        dm.ycc_to_rgb_coef8 = 0;
        dm.rgb_to_lms_coef0 = 5144;
        dm.rgb_to_lms_coef1 = 10478;
        dm.rgb_to_lms_coef2 = 762;
        dm.rgb_to_lms_coef3 = 2545;
        dm.rgb_to_lms_coef4 = 12418;
        dm.rgb_to_lms_coef5 = 1421;
        dm.rgb_to_lms_coef6 = 291;
        dm.rgb_to_lms_coef7 = 1793;
        dm.rgb_to_lms_coef8 = 14298;
        let mapping = template.rpu_data_mapping.as_mut().ok_or("Missing mapping")?;
        for (component, curve) in mapping.curves.iter_mut().enumerate() {
            let poly = curve.polynomial.as_mut().ok_or("Missing polynomial")?;
            poly.poly_coef_int[0][0] = 0;
            poly.poly_coef_int[0][1] = 0;
            let (offset, scale) = if component == 0 { (0.05, 0.70) } else { (0.10, 0.80) };
            poly.poly_coef[0][0] = (offset * ((1u32 << 23) as f64)).round() as u64;
            poly.poly_coef[0][1] = (scale * ((1u32 << 23) as f64)).round() as u64;
        }
    }
    let directory = Path::new(&args[3]);
    fs::create_dir_all(directory)?;
    let mut file = BufWriter::new(File::create(directory.join("rpu.bin"))?);
    for frame in 0..config.length {
        let mut rpu = template.clone();
        rpu.vdr_dm_data.as_mut().ok_or("Missing DM")?.set_scene_cut(frame % 24 == 0);
        rpu.modified = true;
        let encoded = rpu.write_hevc_unspec62_nalu()?;
        let decoded = DoviRpu::parse_unspec62_nalu(&encoded)?;
        if decoded.dovi_profile != template.dovi_profile {
            return Err("Serialized RPU profile mismatch".into());
        }
        if frame == 0 {
            fs::write(directory.join("first-rpu.json"), serde_json::to_vec_pretty(&decoded)?)?;
        }
        file.write_all(&[0, 0, 0, 1])?;
        file.write_all(&encoded[2..])?;
    }
    file.flush()?;
    Ok(())
}
