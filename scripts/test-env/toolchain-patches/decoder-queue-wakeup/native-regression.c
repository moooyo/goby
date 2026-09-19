/*
 * Deterministic regression harness for the FFmpeg n9.0.1 decoder wakeup defect.
 * This includes the actual scheduler and packet queue implementations. Hooks
 * only establish test ordering at their existing receive/wait boundaries.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <pthread.h>
#include <semaphore.h>
#include <signal.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

#include "config.h"
#include "fftools/thread_queue.h"

#ifndef WAKEUP_CANDIDATE
#error "Define WAKEUP_CANDIDATE to 0 for the baseline or 1 for the candidate"
#endif

static int harness_cond_wait(pthread_cond_t *cond, pthread_mutex_t *lock);
static int harness_receive(ThreadQueue *queue, int *stream, void *data, int flags);

/* Header guards keep system declarations outside these test-only wrappers. */
#define pthread_cond_wait harness_cond_wait
#include "fftools/thread_queue.c"
#define tq_receive harness_receive
#include "fftools/ffmpeg_sched.c"
#undef tq_receive
#undef pthread_cond_wait

/* This scheduler dependency is never used by a graph without actual mux tasks. */
int print_sdp(const char *filename)
{
    (void)filename;
    fputs("unexpected SDP initialization in concurrency harness\n", stderr);
    abort();
}

static void require(int condition, const char *message)
{
    if (!condition) {
        fprintf(stderr, "ASSERTION: %s\n", message);
        _exit(2);
    }
}

static void semaphore_wait(sem_t *semaphore)
{
    int ret;
    do {
        ret = sem_wait(semaphore);
    } while (ret < 0 && errno == EINTR);
    require(ret == 0, "semaphore wait");
}

typedef struct Fixture {
    Scheduler scheduler;
    SchDemux demux;
    SchDemuxStream demux_streams[2];
    SchDec decoders[2];
    SchDecOutput decoder_outputs[2];
    SchFilterGraph filter;
    SchFilterIn filter_inputs[2];
    SchMux mux;
    SchMuxStream mux_stream;
    SchedulerNode demux_destinations[2];
    SchedulerNode decoder_destinations[2];
    AVBufferRef *packet_buffers[3];
} Fixture;

enum GateMode {
    GATE_QUEUE_WAIT,
    GATE_BEFORE_RECEIVE,
    GATE_WAITER_WAIT,
};

typedef struct ReceiveCase {
    Fixture *fixture;
    enum GateMode gate;
    sem_t ready;
    sem_t resume;
    int before_receive_observed;
    unsigned wait_phase;
    unsigned wait_phases;
    unsigned overflow_at_wait[2];
    unsigned packet_count;
    int expect_eof;
} ReceiveCase;

static _Thread_local ReceiveCase *current_receiver;

static int harness_cond_wait(pthread_cond_t *cond, pthread_mutex_t *lock)
{
    ReceiveCase *test = current_receiver;
    if (test && test->wait_phase < test->wait_phases) {
        SchDec *decoder = &test->fixture->decoders[0];
        pthread_cond_t *wanted = test->gate == GATE_QUEUE_WAIT
                               ? &decoder->queue->cond : &decoder->waiter.cond;
        if (test->gate != GATE_BEFORE_RECEIVE && cond == wanted &&
            av_container_fifo_can_read(decoder->overflow) ==
                test->overflow_at_wait[test->wait_phase]) {
            test->wait_phase++;
            /* Still holding the real mutex: notification must acquire it after
             * pthread_cond_wait atomically releases it. No sleep orders this. */
            require(sem_post(&test->ready) == 0, "wait-boundary notification");
        }
    }
    return pthread_cond_wait(cond, lock);
}

static int harness_receive(ThreadQueue *queue, int *stream, void *data, int flags)
{
    ReceiveCase *test = current_receiver;
    if (test && test->gate == GATE_BEFORE_RECEIVE &&
        queue == test->fixture->decoders[0].queue && !test->before_receive_observed) {
        test->before_receive_observed = 1;
        /* sch_dec_receive has already checked the choked overflow. */
        require(sem_post(&test->ready) == 0, "pre-receive notification");
        semaphore_wait(&test->resume);
    }
    return tq_receive(queue, stream, data, flags);
}

static void select_input(Fixture *fixture, unsigned input)
{
    Scheduler *scheduler = &fixture->scheduler;
    require(input < 2, "selected graph input");
    require(pthread_mutex_lock(&scheduler->schedule_lock) == 0, "schedule lock");
    fixture->filter.best_input = input;
    schedule_update_locked(scheduler);
    require(pthread_mutex_unlock(&scheduler->schedule_lock) == 0, "schedule unlock");
    require(!atomic_load(&fixture->demux.waiter.choked),
            "shared demuxer must stay unchoked across decoder selection");
    require(!fixture->decoders[0].queue->choked && !fixture->decoders[1].queue->choked,
            "incoming queues must stay unchoked; this is not a queue-choke test");
}

static void fixture_init(Fixture *fixture)
{
    memset(fixture, 0, sizeof(*fixture));
    Scheduler *scheduler = &fixture->scheduler;
    require(pthread_mutex_init(&scheduler->schedule_lock, NULL) == 0, "schedule mutex initialization");
    atomic_init(&scheduler->terminate, 0);
    atomic_init(&scheduler->last_dts, AV_NOPTS_VALUE);
    scheduler->state = SCH_STATE_STARTED;
    scheduler->nb_demux = scheduler->nb_filters = scheduler->nb_mux = 1;
    scheduler->nb_dec = 2;
    scheduler->demux = &fixture->demux;
    scheduler->filters = &fixture->filter;
    scheduler->mux = &fixture->mux;
    scheduler->dec = fixture->decoders;
    fixture->demux.streams = fixture->demux_streams;
    fixture->demux.nb_streams = 2;
    fixture->filter.inputs = fixture->filter_inputs;
    fixture->filter.nb_inputs = 2;
    fixture->mux.streams = &fixture->mux_stream;
    fixture->mux.nb_streams = 1;
    fixture->mux_stream.src = SCH_FILTER_OUT(0, 0);
    fixture->mux_stream.last_dts = 0;
    require(waiter_init(&fixture->demux.waiter) == 0, "demux waiter initialization");
    require(waiter_init(&fixture->filter.waiter) == 0, "filter waiter initialization");
    for (unsigned i = 0; i < 2; i++) {
        SchDec *decoder = &fixture->decoders[i];
        require(waiter_init(&decoder->waiter) == 0, "decoder waiter initialization");
        decoder->src = SCH_DSTREAM(0, i);
        decoder->queue = tq_alloc(1, 4, THREAD_QUEUE_PACKETS);
        decoder->overflow = av_container_fifo_alloc_avpacket(0);
        require(decoder->queue && decoder->overflow, "decoder queue allocation");
        fixture->demux_destinations[i] = SCH_DEC_IN(i);
        fixture->demux_streams[i].dst = &fixture->demux_destinations[i];
        fixture->demux_streams[i].nb_dst = 1;
        fixture->decoder_destinations[i] = SCH_FILTER_IN(0, i);
        decoder->outputs = &fixture->decoder_outputs[i];
        decoder->nb_outputs = 1;
        decoder->outputs[0].dst = &fixture->decoder_destinations[i];
        decoder->outputs[0].nb_dst = 1;
        fixture->filter_inputs[i].src = SCH_DEC_OUT(i, 0);
    }
    select_input(fixture, 1);
    require(atomic_load(&fixture->decoders[0].waiter.choked), "decoder A starts choked");
    require(!atomic_load(&fixture->decoders[1].waiter.choked), "decoder B starts unchoked");
}

static void fixture_destroy(Fixture *fixture)
{
    for (unsigned i = 0; i < 2; i++) {
        require(!fixture->decoders[i].task.thread_running, "decoder thread joined before freeing queue");
        tq_free(&fixture->decoders[i].queue);
        av_container_fifo_free(&fixture->decoders[i].overflow);
        waiter_uninit(&fixture->decoders[i].waiter);
    }
    waiter_uninit(&fixture->demux.waiter);
    waiter_uninit(&fixture->filter.waiter);
    pthread_mutex_destroy(&fixture->scheduler.schedule_lock);
    for (unsigned i = 0; i < FF_ARRAY_ELEMS(fixture->packet_buffers); i++) {
        if (fixture->packet_buffers[i]) {
            require(av_buffer_get_ref_count(fixture->packet_buffers[i]) == 1,
                    "teardown releases every delivered or cancelled packet reference");
            av_buffer_unref(&fixture->packet_buffers[i]);
        }
    }
}

static AVPacket *packet_create(unsigned id)
{
    AVPacket *packet = av_packet_alloc();
    require(packet && av_new_packet(packet, 19) == 0, "packet allocation");
    memset(packet->data, id, packet->size);
    packet->pts = packet->dts = id;
    packet->duration = 1;
    packet->time_base = (AVRational){ 1, 1000 };
    return packet;
}

static void queue_packet(Fixture *fixture, unsigned id, int overflow)
{
    AVPacket *packet = packet_create(id);
    require(id >= 'A' && id <= 'C' && !fixture->packet_buffers[id - 'A'], "unique observed packet identity");
    fixture->packet_buffers[id - 'A'] = av_buffer_ref(packet->buf);
    require(fixture->packet_buffers[id - 'A'] != NULL, "packet ownership observation reference");
    int ret = overflow ? av_container_fifo_write(fixture->decoders[0].overflow, packet, 0)
                       : tq_send(fixture->decoders[0].queue, 0, packet);
    require(ret == 0, "packet transfer to real queue");
    av_packet_free(&packet);
}

static void packet_check(const AVPacket *packet, unsigned id)
{
    require(packet->size == 19 && packet->pts == id && packet->dts == id && packet->duration == 1 &&
            packet->time_base.num == 1 && packet->time_base.den == 1000, "packet metadata/order preserved");
    for (int i = 0; i < packet->size; i++)
        require(packet->data[i] == id, "packet payload preserved without control-packet injection");
}

static void *receive_packets(void *opaque)
{
    ReceiveCase *test = opaque;
    AVPacket *packet = av_packet_alloc();
    require(packet != NULL, "receiver packet allocation");
    current_receiver = test;
    for (unsigned i = 0; i < test->packet_count; i++) {
        require(sch_dec_receive(&test->fixture->scheduler, 0, packet) == 0, "buffered packet advances");
        packet_check(packet, 'A' + i);
        av_packet_unref(packet);
    }
    if (test->expect_eof) {
        require(sch_dec_receive(&test->fixture->scheduler, 0, packet) == AVERROR_EOF,
                "expected normal or explicitly cancelled EOF");
        require(!packet->buf && packet->size == 0, "EOF does not deliver a hidden media packet");
    }
    current_receiver = NULL;
    av_packet_free(&packet);
    return NULL;
}

static void receiver_start(ReceiveCase *test, Fixture *fixture, enum GateMode gate,
                           unsigned packets, int expect_eof)
{
    memset(test, 0, sizeof(*test));
    test->fixture = fixture;
    test->gate = gate;
    test->packet_count = packets;
    test->expect_eof = expect_eof;
    test->wait_phases = 1;
    test->overflow_at_wait[0] = 2;
    require(sem_init(&test->ready, 0, 0) == 0 && sem_init(&test->resume, 0, 0) == 0,
            "test barrier initialization");
    SchTask *task = &fixture->decoders[0].task;
    task->parent = &fixture->scheduler;
    task->node = SCH_DEC_IN(0);
    require(pthread_create(&task->thread, NULL, receive_packets, test) == 0, "receiver thread creation");
    task->thread_running = 1;
}

static void receiver_join(ReceiveCase *test)
{
    SchTask *task = &test->fixture->decoders[0].task;
    if (task->thread_running) {
        require(pthread_join(task->thread, NULL) == 0, "receiver thread joined");
        task->thread_running = 0;
    }
    sem_destroy(&test->ready);
    sem_destroy(&test->resume);
}

enum Scenario {
    OVERFLOW_WAITING,
    NOTIFY_BEFORE_RECEIVE,
    ORDERED_EXISTING_INPUT,
    QUEUED_WHILE_CHOKED,
    NORMAL_EOF_WAITER,
    STOP_QUEUE_WAIT,
    STOP_WAITER,
};

static void run_scenario(enum Scenario scenario)
{
    Fixture fixture;
    ReceiveCase test;
    fixture_init(&fixture);
    queue_packet(&fixture, 'A', 1);
    queue_packet(&fixture, 'B', 1);
    enum GateMode gate = GATE_QUEUE_WAIT;
    if (scenario == NOTIFY_BEFORE_RECEIVE || scenario == ORDERED_EXISTING_INPUT)
        gate = GATE_BEFORE_RECEIVE;
    if (scenario == NORMAL_EOF_WAITER || scenario == STOP_WAITER) {
        tq_send_finish(fixture.decoders[0].queue, 0);
        gate = GATE_WAITER_WAIT;
    }
    if (scenario == ORDERED_EXISTING_INPUT)
        queue_packet(&fixture, 'C', 0);
    unsigned packets = scenario == ORDERED_EXISTING_INPUT || scenario == QUEUED_WHILE_CHOKED ? 3 : 2;
    // sch_dec_receive has already observed input EOF before this waiter. Stop
    // sets terminate, so waiter_wait returns true and that exact branch returns
    // its existing AVERROR_EOF without delivering buffered A/B. Normal EOF must
    // still deliver every packet; the cancellation exception is confined here.
    if (scenario == STOP_WAITER)
        packets = 0;
    receiver_start(&test, &fixture, gate, packets,
                   scenario == NORMAL_EOF_WAITER || scenario == STOP_WAITER);
    semaphore_wait(&test.ready);
    if (scenario == QUEUED_WHILE_CHOKED) {
        /* Publish the next hook target before making C visible to the receiver.
         * The receiver is currently asleep under the actual queue protocol. */
        require(pthread_mutex_lock(&fixture.decoders[0].queue->lock) == 0, "queue barrier lock");
        test.overflow_at_wait[1] = 3;
        test.wait_phases = 2;
        require(pthread_mutex_unlock(&fixture.decoders[0].queue->lock) == 0, "queue barrier unlock");
        queue_packet(&fixture, 'C', 0);
        semaphore_wait(&test.ready);
    }
    if (scenario == STOP_QUEUE_WAIT || scenario == STOP_WAITER) {
        /* Real sch_stop must wake and join the real receiving decoder task.
         * Inert graph nodes have no task parent and therefore need no threads. */
        require(sch_stop(&fixture.scheduler, NULL) == 0, "scheduler stop joins buffered decoder");
        require(fixture.scheduler.state == SCH_STATE_STOPPED, "scheduler stopped");
    } else {
        select_input(&fixture, 0);
        require(!atomic_load(&fixture.decoders[0].waiter.choked), "decoder A unchoked by selected graph input");
        if (gate == GATE_BEFORE_RECEIVE)
            require(sem_post(&test.resume) == 0, "release receiver after notification");
    }
    receiver_join(&test);
    unsigned retained = scenario == STOP_WAITER ? 2 : 0;
    require(av_container_fifo_can_read(fixture.decoders[0].overflow) == retained,
            "only the explicitly cancelled EOF waiter may retain undelivered overflow");
    if (scenario == STOP_WAITER) {
        require(av_buffer_get_ref_count(fixture.packet_buffers[0]) == 2 &&
                av_buffer_get_ref_count(fixture.packet_buffers[1]) == 2,
                "cancelled A/B remain owned by overflow until teardown");
    }
    fixture_destroy(&fixture);
}

#if WAKEUP_CANDIDATE
static void run_interrupt_eof(void)
{
    ThreadQueue *queue = tq_alloc(1, 2, THREAD_QUEUE_PACKETS);
    AVPacket *packet = av_packet_alloc();
    int stream = 99;
    require(queue && packet, "EOF fixture allocation");
    tq_send_finish(queue, 0);
    tq_receive_interrupt(queue);
    tq_receive_interrupt(queue);
    require(tq_receive(queue, &stream, packet, 0) == AVERROR(EINTR) && stream == -1 && !packet->buf,
            "coalesced interrupt consumes no EOF or packet");
    require(tq_receive(queue, &stream, packet, 0) == AVERROR_EOF && stream == 0,
            "per-stream EOF delivered after interruption");
    require(tq_receive(queue, &stream, packet, 0) == AVERROR_EOF && stream == -1,
            "per-stream EOF not duplicated");
    av_packet_free(&packet);
    tq_free(&queue);
}
#endif

static void watchdog(int signal_number)
{
    (void)signal_number;
    _exit(124);
}

int main(int argc, char **argv)
{
    static const struct {
        const char *name;
        enum Scenario scenario;
        int baseline_timeout;
    } cases[] = {
        { "overflow_waiting", OVERFLOW_WAITING, 1 },
        { "notify_before_receive", NOTIFY_BEFORE_RECEIVE, 1 },
        { "ordered_existing_input", ORDERED_EXISTING_INPUT, 0 },
        { "queued_while_choked", QUEUED_WHILE_CHOKED, 1 },
        { "normal_eof_waiter", NORMAL_EOF_WAITER, 0 },
        { "stop_queue_wait", STOP_QUEUE_WAIT, 1 },
        { "stop_waiter", STOP_WAITER, 0 },
    };
    unsigned executed = 0, defects = 0;
    av_log_set_level(AV_LOG_ERROR);
    setvbuf(stdout, NULL, _IONBF, 0);
    for (unsigned i = 0; i < FF_ARRAY_ELEMS(cases) + WAKEUP_CANDIDATE; i++) {
        const char *name = i < FF_ARRAY_ELEMS(cases) ? cases[i].name : "interrupt_eof";
        if (argc == 2 && strcmp(argv[1], name))
            continue;
        require(argc <= 2, "at most one case selector");
        pid_t child = fork();
        require(child >= 0, "bounded case process creation");
        if (!child) {
            signal(SIGALRM, watchdog);
            alarm(3); /* Independent deadlock bound, never an ordering device. */
            if (i < FF_ARRAY_ELEMS(cases))
                run_scenario(cases[i].scenario);
#if WAKEUP_CANDIDATE
            else
                run_interrupt_eof();
#endif
            _exit(0);
        }
        int status;
        pid_t waited;
        do {
            waited = waitpid(child, &status, 0);
        } while (waited < 0 && errno == EINTR);
        require(waited == child, "bounded case reaped");
        int observed = WIFEXITED(status) ? WEXITSTATUS(status) : 128 + WTERMSIG(status);
        int expected = !WAKEUP_CANDIDATE && i < FF_ARRAY_ELEMS(cases) &&
                       cases[i].baseline_timeout ? 124 : 0;
        printf("mode=%s case=%s exit=%d expected=%d result=%s\n",
               WAKEUP_CANDIDATE ? "candidate" : "baseline", name, observed, expected,
               observed == expected ? (observed == 124 ? "DEFECT_REPRODUCED" : "PASS") : "FAIL");
        if (observed != expected)
            return 1;
        defects += observed == 124;
        executed++;
    }
    require(executed > 0, "case selector matched");
    printf("contract=PASS mode=%s cases=%u reproduced_deadlocks=%u\n",
           WAKEUP_CANDIDATE ? "candidate" : "baseline", executed, defects);
    return 0;
}
