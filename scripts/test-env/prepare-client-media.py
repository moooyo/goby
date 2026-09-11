#!/usr/bin/env python3
"""Create owned synthetic media for real-client acceptance on ssh test-env."""

import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys


ROOT = Path('/opt/goby-fixtures/client-m3e')
CONTROL = Path('/opt/goby-test/exec-work-m3e')
MARKER = 'goby-client-media-m3e-v1'
FFMPEG = Path('/opt/goby-toolchains/ffmpeg-9.0.1/bin/ffmpeg')
FFPROBE = FFMPEG.with_name('ffprobe')
MANIFEST = ROOT / 'manifest.json'


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def write_new(path, text, mode=0o644):
    descriptor = os.open(path, os.O_CREAT | os.O_EXCL | os.O_WRONLY | os.O_NOFOLLOW, mode)
    with os.fdopen(descriptor, 'w', encoding='utf-8', newline='\n') as destination:
        destination.write(text)


def make_directory(path):
    path.mkdir(mode=0o755)


def run(arguments, log):
    with log.open('xb') as output:
        os.chmod(log, 0o600)
        result = subprocess.run([str(part) for part in arguments], stdin=subprocess.DEVNULL,
                                stdout=output, stderr=subprocess.STDOUT, timeout=180,
                                env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C.UTF-8'})
    require(result.returncode == 0, 'Media generation failed; retain and inspect the owned log.')


def main():
    require(sys.platform == 'linux' and os.geteuid() == 0 and os.environ.get('SSH_CONNECTION'),
            'Run only as the authorized root operator through ssh test-env.')
    for parent in (ROOT.parent, CONTROL):
        require(parent.resolve() == parent and parent.is_dir() and parent.stat().st_uid == 0,
                'A fixture parent is not the expected root-owned directory.')
    require(json.loads((CONTROL / 'OWNER.json').read_text())['marker'] ==
            'goby-m3e-client-acceptance-v1', 'The private work marker differs.')
    require(stat.S_IMODE(CONTROL.stat().st_mode) == 0o700, 'The work directory is not private.')
    if ROOT.exists() or ROOT.is_symlink():
        require(ROOT.resolve() == ROOT and ROOT.is_dir() and ROOT.stat().st_uid == 0 and
                stat.S_IMODE(ROOT.stat().st_mode) == 0o755, 'The media root is not owned.')
        require((ROOT / '.goby-managed').read_text() == MARKER + '\n', 'The media marker differs.')
        require(MANIFEST.is_file() and not MANIFEST.is_symlink(),
                'Media initialization is incomplete; inspect it without overwriting files.')
        manifest = json.loads(MANIFEST.read_text())
        require(not any(path.is_symlink() for path in ROOT.rglob('*')), 'A fixture contains a symlink.')
        actual = {str(path.relative_to(ROOT)): digest(path) for path in ROOT.rglob('*')
                  if path.is_file() and path != MANIFEST}
        require(manifest['marker'] == MARKER and actual == manifest['files'],
                'The retained fixture membership or bytes changed.')
        print(json.dumps({'status': 'reused', 'manifest': str(MANIFEST), 'files': len(actual)}))
        return
    for binary in (FFMPEG, FFPROBE):
        require(binary.resolve() == binary and binary.is_file() and binary.stat().st_uid == 0,
                'A selected media executable differs.')
    version = subprocess.check_output([str(FFMPEG), '-version'], text=True).splitlines()[0]
    require(version.startswith('ffmpeg version 9.0.1 '), 'The FFmpeg version differs.')
    make_directory(ROOT)
    write_new(ROOT / '.goby-managed', MARKER + '\n')
    movies = ROOT / 'Movies'
    television = ROOT / 'TV'
    music = ROOT / 'Music'
    for directory in (movies, television, music):
        make_directory(directory)
    movie = movies / 'M3e Client Movie.mp4'
    run([FFMPEG, '-nostdin', '-hide_banner', '-loglevel', 'warning', '-n',
         '-f', 'lavfi', '-i', 'testsrc2=size=320x180:rate=30',
         '-f', 'lavfi', '-i', 'sine=frequency=440:sample_rate=48000',
         '-t', '600', '-c:v', 'libx264', '-preset', 'ultrafast', '-crf', '28',
         '-threads', '2', '-pix_fmt', 'yuv420p', '-g', '60',
         '-c:a', 'aac', '-b:a', '96k', '-ac', '2', '-movflags', '+faststart', movie],
        CONTROL / 'media-movie-generation.log')
    os.chmod(movie, 0o644)
    write_new(movies / 'M3e Client Movie.nfo',
              '<movie><title>M3e Client Movie</title><year>2026</year>'
              '<plot>Synthetic 600-second client playback fixture.</plot></movie>\n')
    cues = [(0, 5, 'Opening subtitle'), (88, 98, 'Backward seek subtitle'),
            (118, 128, 'Forward seek subtitle'), (298, 308, 'Midpoint subtitle')]

    def stamp(seconds, separator):
        return f'{seconds // 3600:02d}:{seconds // 60 % 60:02d}:{seconds % 60:02d}{separator}000'

    for extension, separator in (('srt', ','), ('vtt', '.')):
        text = 'WEBVTT\n\n' if extension == 'vtt' else ''
        for index, (start, end, caption) in enumerate(cues, 1):
            text += f'{index}\n{stamp(start, separator)} --> {stamp(end, separator)}\n{caption}\n\n'
        write_new(movies / f'M3e Client Movie.en.{extension}', text)
    series = television / 'M3e Client Series'
    make_directory(series)
    write_new(series / 'tvshow.nfo', '<tvshow><title>M3e Client Series</title></tvshow>\n')
    for season, episodes in ((1, (1, 2)), (2, (1,))):
        directory = series / f'Season {season:02d}'
        make_directory(directory)
        for episode in episodes:
            stem = f'M3e Client Series S{season:02d}E{episode:02d}'
            os.link(movie, directory / (stem + '.mp4'))
            write_new(directory / (stem + '.nfo'),
                      f'<episodedetails><title>Episode {season}-{episode}</title>'
                      f'<season>{season}</season><episode>{episode}</episode></episodedetails>\n')
    for codec, extension in (('libmp3lame', 'mp3'), ('flac', 'flac')):
        target = music / f'M3e Client Audio.{extension}'
        run([FFMPEG, '-nostdin', '-hide_banner', '-loglevel', 'warning', '-n',
             '-f', 'lavfi', '-i', 'sine=frequency=660:sample_rate=48000', '-t', '180',
             '-c:a', codec, '-ac', '2', '-metadata', 'artist=M3e Synthetic Artist',
             '-metadata', 'album=M3e Synthetic Album', '-metadata', f'title=M3e {extension.upper()}',
             target], CONTROL / f'media-{extension}-generation.log')
        os.chmod(target, 0o644)
    probe = json.loads(subprocess.check_output([str(FFPROBE), '-v', 'error', '-show_format',
                                              '-show_streams', '-of', 'json', str(movie)], text=True))
    require(float(probe['format']['duration']) >= 599.9 and
            any(stream.get('codec_name') == 'h264' and stream.get('avg_frame_rate') == '30/1'
                for stream in probe['streams']) and
            any(stream.get('codec_name') == 'aac' for stream in probe['streams']),
            'The synthetic movie does not match its acceptance profile.')
    files = {str(path.relative_to(ROOT)): digest(path) for path in ROOT.rglob('*') if path.is_file()}
    manifest = {'marker': MARKER, 'ffmpegVersion': version, 'ffmpegSha256': digest(FFMPEG),
                'movieProfile': {'durationSeconds': 600, 'fps': 30, 'video': 'h264', 'audio': 'aac'},
                'files': files}
    write_new(MANIFEST, json.dumps(manifest, indent=2, sort_keys=True) + '\n')
    print(json.dumps({'status': 'created', 'manifest': str(MANIFEST), 'files': len(files),
                      'movieBytes': movie.stat().st_size, 'movieSha256': digest(movie)}))


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        print(f'Client media preparation failed ({type(error).__name__}); preserve the owned paths.',
              file=sys.stderr)
        sys.exit(1)
