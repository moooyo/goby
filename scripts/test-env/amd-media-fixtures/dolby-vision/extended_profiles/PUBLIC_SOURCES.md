# Public Dolby Vision source material

This directory's downloader obtains real video content published by Dolby. It does not generate metadata, transcode video, extract frames, or establish color correctness. Run downloads and subsequent verification on the designated remote test environment. Do not add downloaded media to the repository.

## Pinned sources and attribution

The source is [DolbyLaboratories/dolby-vision-contents](https://github.com/DolbyLaboratories/dolby-vision-contents/tree/957c53ac160e11ae581aa1f4fe811a8ad6cf27df), pinned to revision `957c53ac160e11ae581aa1f4fe811a8ad6cf27df`. Its [README](https://github.com/DolbyLaboratories/dolby-vision-contents/blob/957c53ac160e11ae581aa1f4fe811a8ad6cf27df/README.md) explicitly identifies the profile of each video. The 1080p versions are selected to limit disk use.

| Profile | Upstream file under `SolLevante_Netflix/` | Bytes | SHA-256 from Git LFS pointer |
| --- | --- | ---: | --- |
| 5 | `BL_RPU_dvhe-05_1920x1080@24fps_0_6313.mp4` | 141871521 | `87fe0115f3002a621d2380a9f91852ef91a15854a446efe26de8744f77ef5346` |
| 8.4 | `BL_RPU_dvhe-08-84_1920x1080@24fps_0_6313.mp4` | 143530356 | `d81d4c17958946796f30ca28a57776cee187a421649058767c8496cbc1e469bf` |

The publisher's [SolLevante by Netflix.docx](https://github.com/DolbyLaboratories/dolby-vision-contents/blob/957c53ac160e11ae581aa1f4fe811a8ad6cf27df/SolLevante_Netflix/SolLevante%20by%20Netflix.docx) identifies Netflix, Inc. as the original creator, Dolby, Inc. as the modifier, and Creative Commons Attribution 4.0 International as the license for the distributed modifications. The downloader preserves that document and writes `ATTRIBUTION.txt`. Retain attribution and the license link, and describe any subsequent trimming, resizing, or conversion. These files are not covered by the code license of unrelated Dolby projects.

The license document is pinned to 14351 bytes and SHA-256 `4b6690d1e90d9ca86659a382d2cf5aa7218a2b5abac1154bcdd10f56924dc37a`. The downloader requests this file through GitHub's Contents API with the raw-media Accept header and the fixed revision; this avoids relying on access to `raw.githubusercontent.com`. The original Netflix [license text](https://s3.amazonaws.com/download.opencontent.netflix.com/SolLevante/creative-commons-attribution-4-intl-public-license.txt) independently states CC BY 4.0.

## Download on the remote environment

Use a new absolute output path whose parent already exists. By default, only P5 is downloaded. Select P8.4 with `--profile p84`, or explicitly select both:

```sh
python3 fetch_public_sources.py \
  --output /opt/goby-test/player-live-20261004-165c/dv-profiles-fixtures \
  --profile both
```

The script refuses existing output directories, creates `.owner` with `goby-dv-profiles-fixtures-20261005-165c`, and checks available space before downloading. It streams each file to a temporary `.part` file, rejects data exceeding the pinned size, checks the complete SHA-256 and byte count, and only then publishes the MP4. The default needs approximately 277 MB of free space, including a 128 MiB reserve; both videos need approximately 420 MB. No GPU work is performed.

The output contains `sollevante-p5-1080p.mp4` and/or `sollevante-p84-1080p.mp4`, the publisher's license document, attribution, and `provenance.json`. The manifest preserves the fixed revision, source URLs, exact media hashes, expected publisher-declared profiles, and local absolute paths. Its `bitstream_profile_verified` value remains `false`: an independent media probe must check the actual stream before runtime acceptance. If a transfer fails, the script removes only its partial transfer, preserves completed media and provenance, and marks the manifest `failed`. A retry must use a new directory; the script never clears an existing directory.

## Same-title SDR reference

Netflix's public [SolLevante bucket listing](https://s3.amazonaws.com/download.opencontent.netflix.com/?prefix=SolLevante/&delimiter=/) includes an `sdr/` directory. The [SDR listing](https://s3.amazonaws.com/download.opencontent.netflix.com/?prefix=SolLevante/sdr/&delimiter=/) reports `SolLevante_SDR_UHD_24fps.mov`, 16136968701 bytes, last modified 2023-10-06. The source is [SolLevante_SDR_UHD_24fps.mov](https://s3.amazonaws.com/download.opencontent.netflix.com/SolLevante/sdr/SolLevante_SDR_UHD_24fps.mov).

This is the official same-title SDR reference candidate. It is approximately 16.1 GB and is deliberately excluded from the downloader, particularly for workers with limited space. No full-file SHA-256 is published in the listing; its multipart S3 ETag is not a SHA-256 or whole-file MD5. Frame alignment, edit alignment, and intended SDR grading must be checked before adopting it as a pixel oracle. No frames or pixels from it have been checked here.

## Remaining coverage limits

- No officially published real P8.2 video was identified in the investigated Dolby repository, Apple HLS examples, or FFmpeg sample index. P8.2 syntax or generated fixtures must not be described as real publisher video coverage.
- Apple publishes an alternative [P5 HLS example](https://developer.apple.com/streaming/examples/advanced-stream-dv-atmos.html). Its [master playlist](https://devstreaming-cdn.apple.com/videos/streaming/examples/adv_dv_atmos/main.m3u8) declares `dvh1.05.01` variants and same-program `VIDEO-RANGE=SDR` variants. Apple did not provide a redistribution license on the inspected example page; its [site terms](https://www.apple.com/legal/internet-services/terms/site.html) restrict redistribution. The downloader therefore uses the explicitly licensed Dolby/Netflix source.
- FFmpeg's [4khdr collection](https://samples.ffmpeg.org/4khdr/) has SDR/HDR comparisons, but its [readme](https://samples.ffmpeg.org/4khdr/readme.txt) does not establish Dolby Vision profiles for those files. They are not substitutes for P5, P8.4, or P8.2 fixtures.
