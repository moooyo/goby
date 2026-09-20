#include <chromaprint.h>

#include <algorithm>
#include <array>
#include <cinttypes>
#include <climits>
#include <csignal>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <exception>
#include <memory>
#include <new>

#ifdef _WIN32
#include <fcntl.h>
#include <io.h>
#endif

namespace {

constexpr int kAlgorithm = CHROMAPRINT_ALGORITHM_TEST2;
constexpr int kSampleRate = 11025;
constexpr int kChannels = 1;
constexpr uint64_t kMaxSamples = uint64_t(kSampleRate) * 600;
constexpr uint64_t kMaxBytes = kMaxSamples * 2;
constexpr size_t kMaxOutputBytes = 65536;
constexpr char kRevision[] = "aed8eba2202dd9d7b3b0a56c77904cc805490d72";
static_assert(CHROMAPRINT_VERSION_MAJOR == 1 && CHROMAPRINT_VERSION_MINOR == 6 &&
                  CHROMAPRINT_VERSION_PATCH == 1, "Chromaprint headers must be pinned to 1.6.1");
static_assert(kAlgorithm == 1 && CHAR_BIT == 8 && sizeof(int16_t) == 2,
              "The protocol requires algorithm 1 and exact 16-bit PCM");

struct Failure {
    int code;
    const char *message;
};

struct Metadata {
    const char *version;
    int algorithm;
    int rate;
    int channels;
    int step;
    int delay;
};

class JSON {
public:
    void text(const char *value) {
        const size_t length = std::strlen(value);
        if (length > bytes_.size() - size_) {
            throw Failure{4, "output_budget_exceeded"};
        }
        std::memcpy(bytes_.data() + size_, value, length);
        size_ += length;
    }

    void number(uint64_t value) {
        char digits[32];
        const int written = std::snprintf(digits, sizeof(digits), "%" PRIu64, value);
        if (written < 0 || static_cast<size_t>(written) >= sizeof(digits)) {
            throw Failure{4, "integer_encoding_failed"};
        }
        text(digits);
    }

    void write() const {
        if (std::fwrite(bytes_.data(), 1, size_, stdout) != size_ || std::fflush(stdout) != 0) {
            throw Failure{5, "stdout_write_failed"};
        }
    }

private:
    std::array<char, kMaxOutputBytes> bytes_{};
    size_t size_ = 0;
};

bool arguments(int argc, char **argv) {
    if (argc == 2 && std::strcmp(argv[1], "--describe") == 0) {
        return true;
    }
    bool rate = false, channels = false;
    if (argc == 5) {
        for (int index = 1; index < argc; index += 2) {
            if (!rate && std::strcmp(argv[index], "--sample-rate") == 0 &&
                std::strcmp(argv[index + 1], "11025") == 0) {
                rate = true;
            } else if (!channels && std::strcmp(argv[index], "--channels") == 0 &&
                       std::strcmp(argv[index + 1], "1") == 0) {
                channels = true;
            } else {
                throw Failure{2, "invalid_arguments"};
            }
        }
    }
    if (!rate || !channels) {
        throw Failure{2, "invalid_arguments"};
    }
    return false;
}

Metadata metadata(ChromaprintContext *context) {
    const Metadata result{chromaprint_get_version(), chromaprint_get_algorithm(context),
                          chromaprint_get_sample_rate(context), chromaprint_get_num_channels(context),
                          chromaprint_get_item_duration(context), chromaprint_get_delay(context)};
    if (!result.version || std::strcmp(result.version, "1.6.1") != 0 ||
        result.algorithm != kAlgorithm || result.rate != kSampleRate || result.channels != kChannels ||
        result.step <= 0 || result.step > result.rate || result.delay < 0 || result.delay > result.rate * 60) {
        throw Failure{4, "incompatible_chromaprint_metadata"};
    }
    return result;
}

int16_t decode(unsigned char low, unsigned char high) {
    const int32_t value = int32_t(low) | (int32_t(high) << 8);
    return static_cast<int16_t>(value < 32768 ? value : value - 65536);
}

uint64_t feed(ChromaprintContext *context) {
    std::array<unsigned char, 32768> bytes;
    std::array<int16_t, 16385> samples;
    uint64_t total = 0;
    int pending = -1;
    for (;;) {
        // Read only one sentinel byte beyond the limit, never another chunk.
        const size_t wanted = static_cast<size_t>(std::min<uint64_t>(bytes.size(), kMaxBytes - total + 1));
        const size_t count = std::fread(bytes.data(), 1, wanted, stdin);
        if (count == 0) {
            if (std::ferror(stdin) || !std::feof(stdin)) {
                throw Failure{5, "stdin_read_failed"};
            }
            break;
        }
        if (count > kMaxBytes - total) {
            throw Failure{3, "input_budget_exceeded"};
        }
        total += count;
        size_t cursor = 0;
        int sample_count = 0;
        if (pending >= 0) {
            samples[sample_count++] = decode(static_cast<unsigned char>(pending), bytes[cursor++]);
            pending = -1;
        }
        while (cursor + 1 < count) {
            samples[sample_count++] = decode(bytes[cursor], bytes[cursor + 1]);
            cursor += 2;
        }
        if (cursor < count) {
            pending = bytes[cursor];
        }
        if (sample_count && !chromaprint_feed(context, samples.data(), sample_count)) {
            throw Failure{4, "fingerprint_feed_failed"};
        }
        if (std::ferror(stdin)) {
            throw Failure{5, "stdin_read_failed"};
        }
    }
    if (total == 0 || pending >= 0) {
        throw Failure{3, total == 0 ? "empty_input" : "unaligned_pcm_eof"};
    }
    return total;
}

void append_metadata(JSON &output, const Metadata &data, bool describe) {
    output.text("{\"protocol_version\":1,\"mode\":\"");
    output.text(describe ? "describe" : "fingerprint");
    output.text("\",\"chromaprint_version\":\"");
    output.text(data.version); // The exact version was checked before JSON encoding.
    output.text("\",\"chromaprint_revision\":\"");
    output.text(kRevision);
    output.text("\",\"algorithm\":"); output.number(data.algorithm);
    output.text(",\"sample_rate\":"); output.number(data.rate);
    output.text(",\"channels\":"); output.number(data.channels);
    output.text(",\"item_duration_samples\":"); output.number(data.step);
    output.text(",\"delay_samples\":"); output.number(data.delay);
    output.text(",\"first_item_end_sample\":"); output.number(uint64_t(data.delay) + data.step);
    output.text(",\"max_input_samples\":"); output.number(kMaxSamples);
    output.text(",\"max_input_bytes\":"); output.number(kMaxBytes);
    output.text(",\"max_output_bytes\":"); output.number(kMaxOutputBytes);
}

int run(int argc, char **argv) {
    const bool describe = arguments(argc, argv);
#ifdef _WIN32
    if (!describe && _setmode(_fileno(stdin), _O_BINARY) == -1) {
        throw Failure{5, "binary_stdio_failed"};
    }
#endif
    std::unique_ptr<ChromaprintContext, decltype(&chromaprint_free)> context(
        chromaprint_new(kAlgorithm), &chromaprint_free);
    if (!context) {
        throw Failure{4, "fingerprint_context_failed"};
    }
    const Metadata data = metadata(context.get());
    JSON output;
    append_metadata(output, data, describe);
    if (!describe) {
        if (!chromaprint_start(context.get(), kSampleRate, kChannels)) {
            throw Failure{4, "fingerprint_start_failed"};
        }
        const uint64_t input_bytes = feed(context.get());
        const uint64_t input_samples = input_bytes / 2;
        if (!chromaprint_finish(context.get())) {
            throw Failure{4, "fingerprint_finish_failed"};
        }
        int raw_count = 0;
        const uint64_t expected = input_samples > uint64_t(data.delay)
                                      ? (input_samples - data.delay) / data.step : 0;
        if (!chromaprint_get_raw_fingerprint_size(context.get(), &raw_count) || raw_count < 0 ||
            static_cast<uint64_t>(raw_count) != expected ||
            static_cast<uint64_t>(raw_count) > (kMaxSamples - data.delay) / data.step) {
            throw Failure{4, "fingerprint_extent_mismatch"};
        }
        uint32_t *raw = nullptr;
        int received = 0;
        // Upstream's zero-length allocation is platform-dependent. A short
        // aligned input is represented by an empty array without allocating it.
        const bool obtained = raw_count == 0 ||
                              chromaprint_get_raw_fingerprint(context.get(), &raw, &received);
        std::unique_ptr<void, decltype(&chromaprint_dealloc)> allocation(raw, &chromaprint_dealloc);
        if (!obtained || received != raw_count || (raw_count && !raw)) {
            throw Failure{4, "fingerprint_read_failed"};
        }
        output.text(",\"input_samples\":"); output.number(input_samples);
        output.text(",\"input_bytes\":"); output.number(input_bytes);
        output.text(",\"raw_count\":"); output.number(raw_count);
        output.text(",\"raw\":[");
        for (int index = 0; index < raw_count; ++index) {
            if (index) output.text(",");
            output.number(raw[index]);
        }
        output.text("]");
    }
    output.text("}\n");
    output.write();
    return 0;
}

} // namespace

int main(int argc, char **argv) {
#ifdef SIGPIPE
    std::signal(SIGPIPE, SIG_IGN);
#endif
#ifdef _WIN32
    if (_setmode(_fileno(stdout), _O_BINARY) == -1) {
        std::fputs("goby-intro-fingerprint: binary_stdio_failed\n", stderr);
        return 5;
    }
#endif
    try {
        return run(argc, argv);
    } catch (const Failure &failure) {
        std::fprintf(stderr, "goby-intro-fingerprint: %s\n", failure.message);
        return failure.code;
    } catch (const std::bad_alloc &) {
        std::fputs("goby-intro-fingerprint: allocation_failed\n", stderr);
        return 4;
    } catch (const std::exception &) {
        std::fputs("goby-intro-fingerprint: fingerprint_failed\n", stderr);
        return 4;
    } catch (...) {
        std::fputs("goby-intro-fingerprint: unexpected_failure\n", stderr);
        return 4;
    }
}
