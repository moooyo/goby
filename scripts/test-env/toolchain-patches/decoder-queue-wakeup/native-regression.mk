# Run only in the designated remote build environment. This target does not
# configure, modify, or rebuild the FFmpeg source/build trees.
HARNESS_DIR := $(dir $(abspath $(lastword $(MAKEFILE_LIST))))

ifeq ($(strip $(FF_SOURCE)),)
$(error FF_SOURCE must identify the selected baseline or candidate source tree)
endif
ifeq ($(strip $(FF_BUILD)),)
$(error FF_BUILD must identify the completed compatible candidate build tree)
endif
ifeq ($(filter $(WAKEUP_CANDIDATE),0 1),)
$(error WAKEUP_CANDIDATE must be 0 or 1)
endif
ifeq ($(strip $(HARNESS_OUT)),)
$(error HARNESS_OUT must be an absolute private output path)
endif

# Reuse the exact configured compiler and library dependencies. Candidate and
# baseline differ only in fftools; the AV libraries have identical source/ABI.
include $(FF_BUILD)/ffbuild/config.mak

HARNESS_CC ?= $(CC)
HARNESS_FLAGS := -std=c11 -O1 -g -pthread -DHAVE_AV_CONFIG_H \
                 -ffunction-sections -fdata-sections -DWAKEUP_CANDIDATE=$(WAKEUP_CANDIDATE)
HARNESS_LIBS := $(FF_BUILD)/fftools/sync_queue.o \
                -Wl,--start-group $(FF_BUILD)/libavcodec/libavcodec.a \
                $(FF_BUILD)/libavutil/libavutil.a -Wl,--end-group \
                $(EXTRALIBS-avcodec) $(EXTRALIBS-avutil) $(EXTRALIBS) -pthread -lm -ldl

.PHONY: all
all: $(HARNESS_OUT)

$(HARNESS_OUT): $(HARNESS_DIR)/native-regression.c \
                $(FF_SOURCE)/fftools/thread_queue.c $(FF_SOURCE)/fftools/thread_queue.h \
                $(FF_SOURCE)/fftools/ffmpeg_sched.c $(FF_BUILD)/ffbuild/config.mak
	$(HARNESS_CC) $(HARNESS_FLAGS) -I$(FF_SOURCE) -I$(FF_BUILD) $(CPPFLAGS) \
	    $(HARNESS_DIR)/native-regression.c -Wl,--gc-sections $(LDFLAGS) \
	    $(HARNESS_LIBS) -o $(HARNESS_OUT)
