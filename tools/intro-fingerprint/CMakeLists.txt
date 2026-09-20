cmake_minimum_required(VERSION 3.16)
project(goby_intro_fingerprint VERSION 1.0.0 LANGUAGES C CXX)

# This target intentionally does not run Chromaprint's dependency discovery.
# Every compiled source is pinned below; no system Chromaprint, FFmpeg, FFTW,
# resampler, fpcalc, or alternative FFT backend can be selected.
set(CMAKE_C_STANDARD 99)
set(CMAKE_C_STANDARD_REQUIRED ON)
set(CMAKE_CXX_STANDARD 14)
set(CMAKE_CXX_STANDARD_REQUIRED ON)
set(CMAKE_CXX_EXTENSIONS OFF)

file(STRINGS "${CMAKE_CURRENT_SOURCE_DIR}/vendor/SHA256SUMS" vendor_records)
foreach(record IN LISTS vendor_records)
  if(NOT record MATCHES "^([0-9a-f]+)  (chromaprint/[A-Za-z0-9_./-]+)$")
    message(FATAL_ERROR "Invalid pinned-source manifest record")
  endif()
  set(expected "${CMAKE_MATCH_1}")
  set(relative "${CMAKE_MATCH_2}")
  string(LENGTH "${expected}" hash_length)
  if(NOT hash_length EQUAL 64 OR relative MATCHES "[.][.]")
    message(FATAL_ERROR "Invalid pinned-source manifest extent")
  endif()
  file(SHA256 "${CMAKE_CURRENT_SOURCE_DIR}/vendor/${relative}" actual)
  if(NOT actual STREQUAL expected)
    message(FATAL_ERROR "Pinned source differs: ${relative}")
  endif()
endforeach()

set(core "${CMAKE_CURRENT_SOURCE_DIR}/vendor/chromaprint/src")
set(kiss "${core}/3rdparty/kissfft")
add_library(intro_chromaprint STATIC
  "${core}/audio_processor.cpp"
  "${core}/chroma.cpp"
  "${core}/chroma_resampler.cpp"
  "${core}/chroma_filter.cpp"
  "${core}/spectrum.cpp"
  "${core}/fft.cpp"
  "${core}/fft_lib_kissfft.cpp"
  "${core}/fingerprinter.cpp"
  "${core}/image_builder.cpp"
  "${core}/simhash.cpp"
  "${core}/silence_remover.cpp"
  "${core}/fingerprint_calculator.cpp"
  "${core}/fingerprint_compressor.cpp"
  "${core}/fingerprint_decompressor.cpp"
  "${core}/fingerprinter_configuration.cpp"
  "${core}/fingerprint_matcher.cpp"
  "${core}/chromaprint.cpp"
  "${core}/utils/base64.cpp"
  "${kiss}/kiss_fft.c"
  "${kiss}/kiss_fftr.c")
target_include_directories(intro_chromaprint PRIVATE "${core}" "${kiss}" "${CMAKE_CURRENT_SOURCE_DIR}/include")
target_compile_definitions(intro_chromaprint PRIVATE
  HAVE_CONFIG_H CHROMAPRINT_NODLL NDEBUG _USE_MATH_DEFINES
  __STDC_LIMIT_MACROS __STDC_CONSTANT_MACROS)
if(UNIX)
  target_link_libraries(intro_chromaprint PUBLIC m)
endif()

add_executable(goby-intro-fingerprint src/main.cpp)
target_include_directories(goby-intro-fingerprint PRIVATE "${core}")
target_compile_definitions(goby-intro-fingerprint PRIVATE CHROMAPRINT_NODLL)
target_link_libraries(goby-intro-fingerprint PRIVATE intro_chromaprint)
if(MSVC)
  target_compile_options(goby-intro-fingerprint PRIVATE /W4)
else()
  target_compile_options(goby-intro-fingerprint PRIVATE -Wall -Wextra -Wpedantic)
endif()

install(TARGETS goby-intro-fingerprint RUNTIME DESTINATION bin)
install(FILES README.md PROTOCOL.md EXTRACTION.md GEOMETRY.md THIRD_PARTY_NOTICES.md
  DESTINATION share/goby-intro-fingerprint)
install(FILES vendor/chromaprint/LICENSE.md
  DESTINATION share/goby-intro-fingerprint/licenses/chromaprint)
install(FILES vendor/chromaprint/src/3rdparty/kissfft/COPYING
  vendor/chromaprint/src/3rdparty/kissfft/LICENSES/BSD-3-Clause
  DESTINATION share/goby-intro-fingerprint/licenses/kissfft)
