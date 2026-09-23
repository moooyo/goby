/*
 * Exercise the real FFmpeg n9.0.1 progress reporter with scheduler timestamps.
 * Only the output size and CLI option values are fixtures. The timestamp
 * arithmetic, progress serialization, and final AVIO close are production code.
 */
#define _GNU_SOURCE
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define main ffmpeg_cli_main
#include "fftools/ffmpeg.c"
#undef main

int copy_ts;
int print_stats;
int64_t stats_period;

static OutputFile fixture_output;

int64_t of_filesize(OutputFile *output)
{
    if (output != &fixture_output) {
        fputs("unexpected output in progress regression\n", stderr);
        abort();
    }
    return 2880192;
}

int main(int argc, char **argv)
{
    static const int64_t before_origin[] = {
        AV_NOPTS_VALUE, 10083333, 112960000
    };
    static const int64_t after_origin[] = {
        10083333, 112960000, AV_NOPTS_VALUE, 120000000
    };
    static const int64_t final_unknown[] = {
        10083333, AV_NOPTS_VALUE
    };
    static const int64_t without_copyts[] = {
        10083333, AV_NOPTS_VALUE, 120000000
    };
    static const int64_t zero_negative[] = {
        AV_NOPTS_VALUE, -21000, 0, 1, 10083333, AV_NOPTS_VALUE, 10583333
    };
    const int64_t *timestamps = NULL;
    size_t count = 0;
    OutputStream video = { 0 };
    OutputStream *streams[] = { &video };
    OutputFile *outputs[] = { &fixture_output };

    if (argc != 3 || argv[2][0] != '/') {
        fputs("usage: native-regression CASE /private/progress-file\n", stderr);
        return 2;
    }

#define SELECT_CASE(name, values)                         \
    if (!strcmp(argv[1], name)) {                         \
        timestamps = values;                             \
        count = sizeof(values) / sizeof(values[0]);       \
    }
    SELECT_CASE("unknown_before_origin", before_origin)
    SELECT_CASE("unknown_after_origin", after_origin)
    SELECT_CASE("final_unknown", final_unknown)
    SELECT_CASE("copyts_disabled", without_copyts)
    SELECT_CASE("zero_negative_and_recovery", zero_negative)
#undef SELECT_CASE
    if (!timestamps) {
        fputs("unknown progress regression case\n", stderr);
        return 2;
    }

    fixture_output.index = 0;
    fixture_output.nb_streams = 1;
    fixture_output.streams = streams;
    video.file = &fixture_output;
    video.index = 0;
    video.type = AVMEDIA_TYPE_VIDEO;
    atomic_init(&video.packets_written, 123);
    atomic_init(&video.quality, 0);
    output_files = outputs;
    nb_output_files = 1;
    atomic_store(&nb_output_dumped, 1);
    copy_ts = strcmp(argv[1], "copyts_disabled") != 0;
    print_stats = 0;
    stats_period = 0;
    av_log_set_level(AV_LOG_ERROR);

    if (avio_open(&progress_avio, argv[2], AVIO_FLAG_WRITE) < 0) {
        fputs("cannot open private progress output\n", stderr);
        return 2;
    }
    for (size_t index = 0; index < count; index++)
        print_report(index + 1 == count, 0,
                     (int64_t)(index + 1) * AV_TIME_BASE, timestamps[index]);
    if (progress_avio != NULL) {
        fputs("final progress did not close its output\n", stderr);
        return 2;
    }
    if (copy_ts && copy_ts_first_pts != 10083333) {
        fputs("unknown timestamps changed the first copy timestamp\n", stderr);
        return 2;
    }
    return 0;
}
