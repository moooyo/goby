# Run only in the designated remote build environment. The included ffmpeg.c
# and the configured libraries are read directly, without changing build trees.
HARNESS_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

ifeq ($(strip $(FF_SOURCE)),)
$(error FF_SOURCE must identify the selected baseline or candidate source tree)
endif
ifeq ($(strip $(FF_BUILD)),)
$(error FF_BUILD must identify the completed compatible candidate build tree)
endif
ifeq ($(strip $(HARNESS_OUT)),)
$(error HARNESS_OUT must be an absolute private output path)
endif

include $(FF_BUILD)/ffbuild/config.mak

HARNESS_CC ?= $(CC)
# The unpatched subtraction has signed-overflow UB. Explicit two's-complement
# wrapping makes the observed negative control deterministic across compilers;
# this flag is confined to the harness and does not change the media build.
HARNESS_FLAGS := -std=c11 -O1 -g -fwrapv -pthread -DHAVE_AV_CONFIG_H \
                 -ffunction-sections -fdata-sections
HARNESS_LIBS := -Wl,--start-group $(FF_BUILD)/libavformat/libavformat.a \
                $(FF_BUILD)/libavcodec/libavcodec.a $(FF_BUILD)/libavutil/libavutil.a \
                -Wl,--end-group $(EXTRALIBS-avformat) $(EXTRALIBS-avcodec) \
                $(EXTRALIBS-avutil) $(EXTRALIBS) -pthread -lm -ldl

.PHONY: all
all: $(HARNESS_OUT)

$(HARNESS_OUT): $(HARNESS_DIR)/native-regression.c \
                $(FF_SOURCE)/fftools/ffmpeg.c $(FF_BUILD)/ffbuild/config.mak
	$(HARNESS_CC) $(HARNESS_FLAGS) -I$(FF_SOURCE) -I$(FF_BUILD) $(CPPFLAGS) -fwrapv \
	    $(HARNESS_DIR)/native-regression.c -Wl,--gc-sections $(LDFLAGS) \
	    $(HARNESS_LIBS) -o $(HARNESS_OUT)
