/** Read-only source55 deployment authority and ownership pins for one candidate-only UI scope. */
import fs from 'node:fs/promises';
import { constants } from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { TextDecoder } from 'node:util';
import { fileURLToPath } from 'node:url';

const WORK = '/opt/goby-test/exec-work-m3e';
const ROOT = WORK + '/client-library-changed-ui-source55-v7';
const TOOL = WORK + '/client-library-changed-source55-tool-07';
const PRIOR_ROOT = WORK + '/client-library-changed-ui-source55-v2';
const PRIOR_TOOL = WORK + '/client-library-changed-source55-tool-02';
const HISTORY_V3_ROOT = WORK + '/client-library-changed-ui-source55-v3';
const HISTORY_V3_TOOL = WORK + '/client-library-changed-source55-tool-03';
const HISTORY_V4_ROOT = WORK + '/client-library-changed-ui-source55-v4';
const HISTORY_V4_TOOL = WORK + '/client-library-changed-source55-tool-04';
const HISTORY_V5_ROOT = WORK + '/client-library-changed-ui-source55-v5';
const HISTORY_V5_TOOL = WORK + '/client-library-changed-source55-tool-05';
const HISTORY_V6_ROOT = WORK + '/client-library-changed-ui-source55-v6';
const HISTORY_V6_TOOL = WORK + '/client-library-changed-source55-tool-06b';
const HISTORY_V6_NATIVE_PIN = Object.freeze({ path: WORK + '/client-library-changed-source55-native-identity-06/report.json',
  sha256: '05109498040178f8de306c5830c3522792ae89a6c8a68c71ac64b4d45aff8be4' });
const HISTORY_V6_NATIVE_IDENTITY = Object.freeze({ session_id: '71020063951971eff0bb5699d87eb0e0',
  token_sha256: 'ca0338fefd06d4c5fb08797eecfad55f03e37731a8514fddc1482881d9a7c6dd',
  user_id: '0dd576d477e8acea871cb4b06cb11153', kind: 'admin' });
const STATE = WORK + '/client-fixture.json';
const SELF = fileURLToPath(import.meta.url);
const ORIGIN = 'http://127.0.0.1:18196', DIRECT = 'http://127.0.0.1:18198';
const USER = 'ecbbe4cb82403879bc4b4f78894c5738';
const LIBRARY = 'a9993591e72f0f2e7babcbf8b9c50790', ITEM = '268051d3ca734aefcf94e245fb25ad55';
const SERVER = 'c7cfd76b1dee728b2bad523793a37ccb';
const SOURCE_CREDENTIALS_SHA = '0be6df4acef0565537b3b3a6e8c1518f6bed4023bfdfb5dea60b67dd32fee790';
const BOOT = '6bdfc486-7bc8-412f-82b5-70095a09dde7';
const SOURCE = WORK + '/source-attempt-55';
const SOURCE_SHA = '7d2548603e209ebce6154853321147aeec33ccbb40cc12b3ca37418ad765937a';
const CATALOG_SHA = '8e7569c8fe2073ee2ed4c51147f9abc21061d1aac9554843101b826fa5a1cc2b';
const UPGRADE_TOOL = WORK + '/client-schema28-source55-tool-05';
const UPGRADE_MARKER = 'goby-client-schema28-source55-upgrade-v1';
const CONTROLLER_UNIT = 'goby-client-library-changed-ui-source55-controller-v7.service';
const PRIOR_CONTROLLER_UNIT = 'goby-client-library-changed-ui-source55-controller-v2.service';
const PRIOR_WORKER_UNIT = 'goby-client-library-changed-ui-source55-v2.service';
const UPGRADE_ROOT = WORK + '/client-schema28-source55-upgrade-20260912_100845_47bff13c329b';
const UPGRADE_PINS = Object.freeze({
  upgrade_intent: { path: UPGRADE_TOOL + '/intent.json', sha256: 'f1eec6ea8d9aeffdba689f60182ad4ebc75d733f221b4154b5bb75cf05033111' },
  upgrade_report: { path: UPGRADE_ROOT + '/report.json', sha256: '328bc6abbf4d2fe0559a34ed62d9e7d9cf7b77c075c583f9070d7aa9761c8832' },
  upgrade_attestation: { path: UPGRADE_ROOT + '/attestation.json', sha256: 'ec20d286a1998f0b27667603819853129e88b91579a97a25141bd6258e450032' },
  current_snapshot: { path: UPGRADE_ROOT + '/after-full.json', sha256: '7b61f5c94c440e5f6fefc58f0ef04ee2bd3c8f118510ea727f6741c46488412d' },
});
const PRIOR_PINS = Object.freeze({
  prior_input: { path: PRIOR_ROOT + '/input.json', sha256: 'c88794dbd9bdedcb6c16fea4dd8a7d8af2d09012728084b30e24701db5872c73' },
  prior_browser_report: { path: PRIOR_ROOT + '/browser/report.json', sha256: 'c980edd73bbf08c6190391f2c5368abd7973c5c1381bcec70d7be93477f8c743' },
  prior_controller_report: { path: PRIOR_ROOT + '/report.json', sha256: '467f473ef20b1b05bfc76c863b41f69eeb77f1f2a783f57d6549beaa46f28104' },
  prior_terminal: { path: WORK + '/client-library-changed-source55-execution-02/failed-terminal.json',
    sha256: 'b23a1a156e781c771e3bb4b1ba31bfb048e29e77504569d129397c79445265d6' },
  prior_before_snapshot: { path: PRIOR_ROOT + '/before-full.json', sha256: '36ff8634f85a58841c1c6e4558de5e4dcfea1a842bae8e40a26c1d37c12ff32f' },
  prior_after_snapshot: { path: PRIOR_ROOT + '/after-full.json', sha256: 'a13f976b7097e33337527ef2cf10ad9203d755e0fd2cec43307edfa6efbfe8bc' },
});
const PRIOR_TERMINAL_PINS = Object.freeze({
  independent_snapshot: { path: WORK + '/client-library-changed-source55-execution-02/independent-after-full.json',
    sha256: '6dfe6cbbf2288b21a66466719067b2c0d063bb588e693e06479d20da4feb9cd4' },
  scope_files: { path: WORK + '/client-library-changed-source55-execution-02/failed-scope-files.json',
    sha256: '3ef622b6f1724c996177ab7a2673b34c9d16a41608957f8546c0ac24d2085eaf' },
  seal_script: { path: WORK + '/client-library-changed-source55-execution-02/seal-failed-terminal.py',
    sha256: '3f9bcdbf83c62b5e5cd5ead3fc77d9398b9754ca255ca3e97298cf79fb8d2c8c' },
  controller_source: { path: PRIOR_TOOL + '/observe-client-library-changed-source55.py',
    sha256: '57fe02b0fc32d335bb08a89e3f5c31c9121a7af44997c31e1329761b82701f25' },
});
const HISTORY_V3_PINS = Object.freeze({
  prior_input: { path: HISTORY_V3_ROOT + '/input.json', sha256: 'eab8c899d06b525271afec345d4137b3e0adec70bb5e8ecf911fdf4dae7e9f6e' },
  prior_browser_report: { path: HISTORY_V3_ROOT + '/browser/report.json', sha256: '9360e3b5c60b16f714b80b2da3f1f7a31d58313e196abee99dfbb4f8e3bc9d1f' },
  prior_controller_report: { path: HISTORY_V3_ROOT + '/report.json', sha256: 'e9b931c568c98e4438917d8c204922267b0931a2de4c9f6bba40f2155c196fb4' },
  prior_terminal: { path: WORK + '/client-library-changed-source55-execution-03/failed-terminal.json',
    sha256: 'e53e9777337c8c0b33a0cc86dad2e22c79d5e7f2601aa64dd68c6ed91cd9ecf1' },
  prior_before_snapshot: { path: HISTORY_V3_ROOT + '/before-full.json', sha256: '10563f9b12e62a321bbda67c49bfbfc1a6e3d2c2e304d3c2e1e7d64502b2c897' },
  prior_after_snapshot: { path: HISTORY_V3_ROOT + '/after-full.json', sha256: 'a84e5e480a70b7d84e49001aaae791a522cd38c6b1055dcb503f0312f933c2dd' },
});
const HISTORY_V3_TERMINAL_PINS = Object.freeze({
  independent_snapshot: { path: WORK + '/client-library-changed-source55-execution-03/independent-after-full.json',
    sha256: '668975cd51c66888fb363ba273fe15dec70646d87e9549adf0fbe79c20b4f3f0' },
  scope_files: { path: WORK + '/client-library-changed-source55-execution-03/failed-scope-files.json',
    sha256: 'a27ba50b166ef0752e466960d4eab94c457e699228daf65eeed366305b5a287f' },
  seal_script: { path: WORK + '/client-library-changed-source55-execution-03/seal-failed-terminal.py',
    sha256: '525caf668f68d365599b067d07d8f5e635df1bd011359d8c807014697e87c41d' },
  controller_source: { path: HISTORY_V3_TOOL + '/observe-client-library-changed-source55.py',
    sha256: '68753cc567ceb138b1a2cfd693020b42556448cc5da0821c4ead1f8cf93700f2' },
});
const HISTORY_V3_PREDECESSORS = Object.freeze({
  predecessor_terminals: [
    { path: WORK + '/client-library-changed-source55-execution-01/failed-terminal.json', sha256: 'ecdc5fdf48bf7d471676e6786d52ed61ce1be3053537ce36fde5d44a3d1552a6' },
    PRIOR_PINS.prior_terminal,
  ],
  predecessor_inventories: [
    { path: WORK + '/client-library-changed-source55-execution-01/failed-scope-files.json', sha256: '62cc954df4141a3ddb3faf9e99772a9055f7c5f864f68ee1d30728ae00144872' },
    PRIOR_TERMINAL_PINS.scope_files,
  ],
  discovery_failure: { path: HISTORY_V3_ROOT + '/browser/discovery-failure.json',
    sha256: '23b42c459572f25fd17f18b86eacc3b75c7553020521df6ea0ab6ec43bfb609f' },
});
const HISTORY_V4_PINS = Object.freeze({
  prior_input: { path: HISTORY_V4_ROOT + '/input.json', sha256: '963bf8d50a68ffd9534146f66d53df10b099818307f2496517cc89c97161a94d' },
  prior_browser_report: { path: HISTORY_V4_ROOT + '/browser/report.json', sha256: 'f99ff64ced6abb41a0c5f913a2a66cf18e36bb6527a46a43172f0e7c78a89cf4' },
  prior_controller_report: { path: HISTORY_V4_ROOT + '/report.json', sha256: '6d4acb05e4c0f7328c1cc28c41b17062c08bdd05eb441bcd70786649fa6d4a88' },
  prior_terminal: { path: WORK + '/client-library-changed-source55-execution-04/failed-terminal.json',
    sha256: '021a36cc9e6311107acf751b1bba7ca6b2bf1e6618c4a32b6b48c17866404012' },
  prior_before_snapshot: { path: HISTORY_V4_ROOT + '/before-full.json', sha256: 'f37fe8d9134a98cc780ecafb20053a0c140429efc18df47f5e4106ffefa501f9' },
  prior_after_snapshot: { path: HISTORY_V4_ROOT + '/after-full.json', sha256: 'bd992669d5c9ade22768f210009e983b46384c2611a8d8d91986cb729f41236f' },
});
const HISTORY_V4_TERMINAL_PINS = Object.freeze({
  independent_snapshot: { path: WORK + '/client-library-changed-source55-execution-04/independent-after-full.json',
    sha256: '5ceeb7916191ceaa223a27062ed399614e906481528a9038400eed3c64c882f1' },
  scope_files: { path: WORK + '/client-library-changed-source55-execution-04/failed-scope-files.json',
    sha256: '2618849ab3caa0d8756f372e0a3c9022f6d3bc7b2ef325a8e623bcd27172e407' },
  seal_script: { path: WORK + '/client-library-changed-source55-execution-04/seal-failed-terminal.py',
    sha256: 'bc078d418dbc68c1909fa6f9795c9217111e4787d393de7109c4b25a02c3f80f' },
  controller_source: { path: HISTORY_V4_TOOL + '/observe-client-library-changed-source55.py',
    sha256: '9fcfc06b6c9e2922064849db65e4a13c05b2b3f13835cf7b28e9a6b3a78a953b' },
});
const HISTORY_V4_DIAGNOSTICS = Object.freeze({
  discovery_failure: { path: HISTORY_V4_ROOT + '/browser/discovery-failure.json',
    sha256: '2c9153e4dee96d1d704552473240692717d272b9397a6f4682885eff1b2f97b6' },
  screenshot: { path: HISTORY_V4_ROOT + '/browser/discovery-failure.png',
    sha256: '07cab956ecc046fa0ad2e634a188bf907b15ff289575ab6160521e933ddead10' },
});
const HISTORY_V4_PREDECESSORS = Object.freeze({
  predecessor_terminals: [...HISTORY_V3_PREDECESSORS.predecessor_terminals, HISTORY_V3_PINS.prior_terminal],
  predecessor_inventories: [...HISTORY_V3_PREDECESSORS.predecessor_inventories, HISTORY_V3_TERMINAL_PINS.scope_files],
  discovery_failure: HISTORY_V4_DIAGNOSTICS.discovery_failure,
  discovery_screenshot: HISTORY_V4_DIAGNOSTICS.screenshot,
});
const HISTORY_V5_PINS = Object.freeze({
  prior_input: { path: HISTORY_V5_ROOT + '/input.json', sha256: '093dfa5fc0d82ba509a0092c425ce64d33eebf3625441751427248065bdf2176' },
  prior_browser_report: { path: HISTORY_V5_ROOT + '/browser/report.json', sha256: '86e0dafc48e8e9a54b83c4588f7a06cabf951eab4a7d250b3d326cd9271d2abe' },
  prior_controller_report: { path: HISTORY_V5_ROOT + '/report.json', sha256: '154bd9857248eec76e921b2f34f3896e9a1fa73aeb078a64d03d06b54ca7e444' },
  prior_terminal: { path: WORK + '/client-library-changed-source55-execution-05/failed-terminal.json',
    sha256: '348be31960bb19a9905c4cef2b6f19df9af25466e28959f00d1bd5e0a6a2183d' },
  prior_before_snapshot: { path: HISTORY_V5_ROOT + '/before-full.json', sha256: '6bd9998f16407711b5b3cb46a4f3b233822a7147203f353ff55bb2092950e46e' },
  prior_after_snapshot: { path: HISTORY_V5_ROOT + '/after-full.json', sha256: 'a3d633f0d351001eff02d013c2ccc224adec9c5f5a659cab7bad3f4e9a4a20d6' },
});
const HISTORY_V5_TERMINAL_PINS = Object.freeze({
  independent_snapshot: { path: WORK + '/client-library-changed-source55-execution-05/independent-after-full.json',
    sha256: '6724f398e0aa0303005b2f77c629491b1444c21f78deb021849b67eb7a4d1fe5' },
  scope_files: { path: WORK + '/client-library-changed-source55-execution-05/failed-scope-files.json',
    sha256: '190b6ea5aaad2d4cb7ab8184623b5abfd42dc5493a6c640e5de4836399ae3970' },
  seal_script: { path: WORK + '/client-library-changed-source55-execution-05/seal-failed-terminal.py',
    sha256: 'f4b1d863886d1efd3aba7d777683f29652fcef31508122b81abc5cc80628179b' },
  controller_source: { path: HISTORY_V5_TOOL + '/observe-client-library-changed-source55.py',
    sha256: '9a699ccf5fce8b73d0d31a32fb030cf57d24a078041c7af32298be21d7ac2c32' },
});
const HISTORY_V5_DIAGNOSTICS = Object.freeze({
  discovery_failure: { path: HISTORY_V5_ROOT + '/browser/discovery-failure.json',
    sha256: '63fd1776fd0cc6889e081431e457afaeb936f83a1253795944b10223fb68ffc9' },
  screenshot: { path: HISTORY_V5_ROOT + '/browser/discovery-failure.png',
    sha256: '07cab956ecc046fa0ad2e634a188bf907b15ff289575ab6160521e933ddead10' },
});
const HISTORY_V5_PREDECESSORS = Object.freeze({
  predecessor_terminals: [...HISTORY_V4_PREDECESSORS.predecessor_terminals, HISTORY_V4_PINS.prior_terminal],
  predecessor_inventories: [...HISTORY_V4_PREDECESSORS.predecessor_inventories, HISTORY_V4_TERMINAL_PINS.scope_files],
  discovery_failure: HISTORY_V5_DIAGNOSTICS.discovery_failure,
  discovery_screenshot: HISTORY_V5_DIAGNOSTICS.screenshot,
});
const HISTORY_V5_GUARD_EVIDENCE = Object.freeze({
  initial_controller_errors: 1, initial_controller_passed: 73, initial_controller_status: 'failed', initial_controller_tests: 74,
  initial_report: { path: WORK + '/client-library-changed-source55-final-verification-05/guards-report.json',
    sha256: 'ce6de3f403e40d5408e06a1b813c3b0886ba398ca3698b972587cbfcc5d0df9d' },
  isolated_controller_copy: { path: WORK + '/client-library-changed-source55-controller-guards-05b/observe-client-library-changed-source55.py',
    sha256: '9a699ccf5fce8b73d0d31a32fb030cf57d24a078041c7af32298be21d7ac2c32' },
  launch_prerequisites: {
    python_preflight: { path: WORK + '/client-library-changed-source55-preflight-05/report.json',
      sha256: '607ffe77bb1f5eed1897cfbd84965f757c1669790f6ed122fcb1dce9100266fc' },
    real_documents: { path: WORK + '/client-library-changed-source55-setup-diagnosis-06/stdout.json',
      sha256: '45fba63990e53672286d78ec230e945d895c700bd6f3a9d046cddedf9ce924ce' },
    repaired_guards: { path: WORK + '/client-library-changed-source55-controller-guards-05b/report.json',
      sha256: '100802bbdfa57812bc1890cdd3df9f0a169b5f4b5755be1184edc816b47899fe' },
  },
  old_failed_guard_preserved: true,
  old_guard_source: { path: HISTORY_V5_TOOL + '/test-observe-client-library-changed-source55.py',
    sha256: '67cd8a1aca66609758644f751ec681c4389aa189444f904565b7ea3b3b5287db' },
  repaired_controller_errors: 0, repaired_controller_status: 'passed', repaired_controller_tests: 75,
  repaired_guard_source: { path: WORK + '/client-library-changed-source55-controller-guards-05b/test-observe-client-library-changed-source55.py',
    sha256: '62cf239d55d3db49ac60f16fc832cd59cf8f329c1c1919c347ed7111510cbc95' },
  repaired_report: { path: WORK + '/client-library-changed-source55-controller-guards-05b/report.json',
    sha256: '100802bbdfa57812bc1890cdd3df9f0a169b5f4b5755be1184edc816b47899fe' },
  retained_passed_counts: { actor: 127, driver: 64, loader: 559 },
  runtime_controller_source: HISTORY_V5_TERMINAL_PINS.controller_source,
  runtime_sources_unchanged: true,
});
const HISTORY_V6_PINS = Object.freeze({
  prior_input: { path: HISTORY_V6_ROOT + '/input.json', sha256: 'db95204f33d2a7b5549ee5561fefa8ec074747b924489781e6266f1b08566b6e' },
  prior_browser_report: { path: HISTORY_V6_ROOT + '/browser/report.json', sha256: '91bf25713d2b173fc4f6d337a792aeeb7cebbcf1ac38881ced9fa7346a6e61d9' },
  prior_controller_report: { path: HISTORY_V6_ROOT + '/report.json', sha256: '1ffdfab90a9231d7d387a4d2d90f61bdccdffe246b3f1d10a303467d810cfb5e' },
  prior_terminal: { path: WORK + '/client-library-changed-source55-execution-06/failed-terminal.json',
    sha256: '7ea63700f72924318e21e84c061fe1cda3f6d7f04373eaddf78a7f5abea1012d' },
  prior_before_snapshot: { path: HISTORY_V6_ROOT + '/before-full.json', sha256: '8041cea998a0bb065c54cdf0694b097ba4702cdaf345fe56400ed41235efb5c9' },
  prior_after_snapshot: { path: HISTORY_V6_ROOT + '/after-full.json', sha256: '185ea8df81cc228c329f108354cfaa098824a863a4db1a24a7e2acacdf376fa3' },
});
const HISTORY_V6_TERMINAL_PINS = Object.freeze({
  independent_snapshot: { path: WORK + '/client-library-changed-source55-execution-06/independent-after-full.json',
    sha256: 'b5d792af3b3f8dc854b58bdabc7cfaec0ec87ee4382c0d85250b1b38af693c93' },
  scope_files: { path: WORK + '/client-library-changed-source55-execution-06/failed-scope-files.json',
    sha256: 'ac5150779b1e8b5e8467ca929729773ad80c575c299b58d50bcaa857fb67630d' },
  seal_script: { path: WORK + '/client-library-changed-source55-execution-06/seal-failed-terminal.py',
    sha256: 'ecbba4bbaa685f95db96178c707200b76aee8c605e1bf40603ceb42bb83f38a7' },
  controller_source: { path: HISTORY_V6_TOOL + '/observe-client-library-changed-source55.py',
    sha256: 'aebd74bea66f02ffdb054e2cc671a343fe6c81cc1fd918cabbc187257aa41334' },
});
const HISTORY_V6_DISCOVERY = Object.freeze({
  accepted: { path: HISTORY_V6_ROOT + '/accepted-stage-discovery.json', sha256: '54f8e01e1156b9898f4bd5b3e5e24f25bf6c1addf5c794fa2416a779e2561140' },
  stage: { path: HISTORY_V6_ROOT + '/browser/stage-discovery.json', sha256: 'f8499f240bf4e3968c252e89d476216570ac3a27bbc8eb43bd32bf44e2d7cd60' },
  session_private: { path: HISTORY_V6_ROOT + '/browser/session-private.json', sha256: '86732ce8bb705ae8a766c63734e9ab217e57186c047d92f9033f5fc19bfb1aed' },
  abort: { path: HISTORY_V6_ROOT + '/abort.json', sha256: '119c8068409ece1a84a5716bef805f62d9de3c700014752c299972109985a44f' },
  browser_abort: { path: HISTORY_V6_ROOT + '/browser/abort.json', sha256: 'b6025b624f8b9db49cd38745dd7a9f016533c86c55be0e2cfe13c52b5d6e9cfe' },
});
const HISTORY_V6_PREDECESSORS = Object.freeze({
  predecessor_terminals: [...HISTORY_V5_PREDECESSORS.predecessor_terminals, HISTORY_V5_PINS.prior_terminal],
  predecessor_inventories: [...HISTORY_V5_PREDECESSORS.predecessor_inventories, HISTORY_V5_TERMINAL_PINS.scope_files],
});
const HISTORY_V6_TERMINAL_FACTS = Object.freeze({
  "auxiliary_units": {
    "goby-client-library-changed-source55-dom-v6.service": {
      "properties": {
        "ActiveState": "failed",
        "ControlGroup": "",
        "ExecMainStatus": "1",
        "InvocationID": "877ce871b5a74b5fa0f83ca7464d7bbc",
        "MainPID": "0",
        "Result": "exit-code",
        "SubState": "failed"
      },
      "receipt": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-dom-verification-06/failed-terminal.json",
        "sha256": "c05cfb5546db18631e437ccd75323dd593124d29788d5206a42fa7e5beb9b2f6"
      },
      "recursive_cgroup": {
        "exists": false,
        "files_checked": 0,
        "path": "/sys/fs/cgroup/system.slice/goby-client-library-changed-source55-dom-v6.service",
        "processes": 0
      }
    },
    "goby-client-library-changed-source55-dom-v6b.service": {
      "properties": {
        "ActiveState": "active",
        "ControlGroup": "",
        "ExecMainStatus": "0",
        "InvocationID": "668d72b627214f9ca11a999283b27052",
        "MainPID": "0",
        "Result": "success",
        "SubState": "exited"
      },
      "receipt": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-dom-verification-06b/terminal.json",
        "sha256": "1b25b5ed9c8d6b1ce63ef8ac72aa2eed5c474d6450e8cb1f36155c58a1bce45f"
      },
      "recursive_cgroup": {
        "exists": false,
        "files_checked": 0,
        "path": "/sys/fs/cgroup/system.slice/goby-client-library-changed-source55-dom-v6b.service",
        "processes": 0
      }
    }
  },
  "failure": {
    "failure_type": "ObservationError",
    "reason": "A timestamp is not UTC.",
    "stage": "execution_discovery"
  },
  "native_authentication": {
    "exact401_status": 401,
    "login_response_complete": true,
    "logout_status": 204,
    "metadata_requests": 0,
    "native_login_failure_type": "ObservationError",
    "native_login_validation_passed": false,
    "owned_session_closed": true,
    "private": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-session-private.json",
      "sha256": "c4f7dfcdc31031e03205b37e1be7d03c2106881b736342155bb88346c46bc41f"
    },
    "received_cookie_owned": true,
    "received_header": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-cookie-received-private.json",
      "sha256": "d82b0aae258c72eb8b486259b0d1ba9087f808e244e8d2b869e79e81b63b51fe"
    },
    "requests": {
      "exact401": {
        "complete": true,
        "intent": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-exact401-intent.json",
          "sha256": "c803161d8fbfad1038ed6583801ea0429cd9300cee301fdc15445d0c814ad23b"
        },
        "result": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-exact401-result.json",
          "sha256": "fcc3b23cf61075cc79e25df1a21f46de765a0e467111e05caa74253f8d7ea701"
        },
        "status": 401
      },
      "login": {
        "complete": true,
        "intent": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-login-intent.json",
          "sha256": "7b5e170e0fa6f9b2804bd275e0c0915cef6887beb5605d02b1c26b338bff085c"
        },
        "result": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-login-result.json",
          "sha256": "a47b15514956048dd329c544b1c39fd2d366c56d4fa4d87adcedf4590ee921b2"
        },
        "status": 200
      },
      "logout": {
        "complete": true,
        "intent": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-logout-intent.json",
          "sha256": "0e9de3666eda24eacece40cc4eb794cf2f75e46b3a134badf9258e72a4f39c85"
        },
        "result": {
          "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-logout-result.json",
          "sha256": "3b4a26aedffe4d19d78d291684aeb1b3ce949e72fb40f077c4c2e700893cac67"
        },
        "status": 204
      }
    }
  },
  "prerequisites": {
    "pure_guards": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-final-verification-06b/guards-report.json",
      "sha256": "6fe209c8913fba976e9c2fe86ce943bf4b0d23a57af26cb112adbbb4c82af5f6"
    },
    "python_preflight": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-preflight-06b/report.json",
      "sha256": "54cd34c57ccb715d1b64284b11eec7cd2ae2fbe04daaf0d2374843c3f3e19102"
    },
    "real_documents": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-setup-diagnosis-07/stdout.json",
      "sha256": "d0829b4c50ded8e050ad9d3836477189f844fe1a637473fee3193c96aaaf1cda"
    },
    "real_dom": {
      "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-dom-verification-06b/terminal.json",
      "sha256": "1b25b5ed9c8d6b1ce63ef8ac72aa2eed5c474d6450e8cb1f36155c58a1bce45f"
    }
  }
});
const HISTORY_V6_TERMINAL_KEYS = Object.freeze(["accepted_browser_stages","all_prior_scopes_preserved","automatic_retry","auxiliary_units","baseline_chain_verified","before_snapshot","browser","browser_report","candidate","candidate_preserved","captured_at","cleanup","cleanup_needed","cleanup_performed","client_acceptance","controller_source","cumulative_totals","current_matches_prior_after","dispatched_native_intents","exact_owned_additions_retained","failed_units","failure","full_m3_complete","history_preservation","http_requests","independent_snapshot","input","ledger","library_changed_client_acceptance","marker","media_fact_sha256","media_preserved","metadata_requests","native_authentication","observed_run_status","old_rows_sequences_private_preserved","owned_sessions_closed","phase","predecessor_inventories","predecessor_terminals","predecessor_units_preserved","prerequisites","primary_fact_sha256","primary_invocation_id","primary_preserved","primary_process","prior_after_snapshot","prior_baseline","prior_failed_verification_preserved","report","reserved_native_intents","restoration","schema","scope","scope_files","scope_files_unchanged","seal_script","service_writes","sql_business_writes","status","tool","upgrade_authority_snapshot","version"]);
const HISTORY_V6_TERMINAL_SCALARS = Object.freeze({
  "all_prior_scopes_preserved": true,
  "automatic_retry": false,
  "baseline_chain_verified": true,
  "candidate_preserved": true,
  "cleanup": "owned_sessions_already_closed",
  "cleanup_needed": false,
  "cleanup_performed": false,
  "client_acceptance": false,
  "current_matches_prior_after": true,
  "exact_owned_additions_retained": true,
  "full_m3_complete": false,
  "http_requests": 0,
  "library_changed_client_acceptance": false,
  "marker": "goby-source55-failed-ui-terminal-v6",
  "media_fact_sha256": "0f21473c43a050ad54f8985ee57e98addc6420e0cf33d6ee115db8cf8c0eff7d",
  "media_preserved": true,
  "metadata_requests": 0,
  "observed_run_status": "failed",
  "old_rows_sequences_private_preserved": true,
  "owned_sessions_closed": true,
  "phase": "discovery",
  "predecessor_units_preserved": true,
  "primary_fact_sha256": "0882d96f8b61c5586ce514a4c320a9bc933c2610cf55f24bfbec80237e77da3a",
  "primary_invocation_id": "bb94d74b475f4382a6ec6f6df181dd74",
  "primary_preserved": true,
  "prior_failed_verification_preserved": true,
  "restoration": "not_required",
  "schema": 28,
  "scope": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6",
  "scope_files_unchanged": true,
  "service_writes": 0,
  "sql_business_writes": false,
  "status": "failed_scope_sealed",
  "tool": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-tool-06b",
  "version": 1
});
const HISTORY_V6_NATIVE_FACTS = Object.freeze({
  "after_snapshot": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/after-full.json",
    "sha256": "185ea8df81cc228c329f108354cfaa098824a863a4db1a24a7e2acacdf376fa3"
  },
  "automatic_retry": false,
  "before_snapshot": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/before-full.json",
    "sha256": "8041cea998a0bb065c54cdf0694b097ba4702cdaf345fe56400ed41235efb5c9"
  },
  "captured_at": "2026-09-12T13:35:36.931398+00:00",
  "client_acceptance": false,
  "exact401_status": 401,
  "full_m3_complete": false,
  "header_status": 200,
  "http_requests": 0,
  "input": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/input.json",
    "sha256": "db95204f33d2a7b5549ee5561fefa8ec074747b924489781e6266f1b08566b6e"
  },
  "kind": "admin",
  "library_changed_client_acceptance": false,
  "lifetime_hours": 24,
  "login_response_complete": true,
  "logout_status": 204,
  "marker": "goby-source55-native-identity-v1",
  "native_audits_verified": true,
  "native_login_failure_type": "ObservationError",
  "native_login_validation_passed": false,
  "native_private": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-session-private.json",
    "sha256": "c4f7dfcdc31031e03205b37e1be7d03c2106881b736342155bb88346c46bc41f"
  },
  "new_administrator_sessions": 1,
  "owned_session_closed": true,
  "projection_source": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-native-identity-06/project-native-identity.py",
    "sha256": "83f90e3029362bcc6fd40ad8c0ea421891da1cfc775b9ccdb2e975e28246f866"
  },
  "received_cookie_owned": true,
  "received_header": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-cookie-received-private.json",
    "sha256": "d82b0aae258c72eb8b486259b0d1ba9087f808e244e8d2b869e79e81b63b51fe"
  },
  "requests": {
    "exact401": {
      "body_bytes": 149,
      "body_sha256": "4350d3bf9c7f84a0507a0bcadc414bf7e7de40648527e4543a9a394463f7f12b",
      "complete": true,
      "intent": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-exact401-intent.json",
        "sha256": "c803161d8fbfad1038ed6583801ea0429cd9300cee301fdc15445d0c814ad23b"
      },
      "result": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-exact401-result.json",
        "sha256": "fcc3b23cf61075cc79e25df1a21f46de765a0e467111e05caa74253f8d7ea701"
      },
      "status": 401
    },
    "login": {
      "body_bytes": 272,
      "body_sha256": "78f9c5001ee426b92c26dd797df7fb93f89714878d6b1b24d1f735706164de6b",
      "complete": true,
      "intent": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-login-intent.json",
        "sha256": "7b5e170e0fa6f9b2804bd275e0c0915cef6887beb5605d02b1c26b338bff085c"
      },
      "result": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-login-result.json",
        "sha256": "a47b15514956048dd329c544b1c39fd2d366c56d4fa4d87adcedf4590ee921b2"
      },
      "status": 200
    },
    "logout": {
      "body_bytes": 0,
      "body_sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
      "complete": true,
      "intent": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-logout-intent.json",
        "sha256": "0e9de3666eda24eacece40cc4eb794cf2f75e46b3a134badf9258e72a4f39c85"
      },
      "result": {
        "path": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6/native-logout-result.json",
        "sha256": "3b4a26aedffe4d19d78d291684aeb1b3ce949e72fb40f077c4c2e700893cac67"
      },
      "status": 204
    }
  },
  "scope": "/opt/goby-test/exec-work-m3e/client-library-changed-ui-source55-v6",
  "service_writes": 0,
  "session_id": "71020063951971eff0bb5699d87eb0e0",
  "source_files_unchanged": true,
  "sql_queries": 0,
  "status": "verified",
  "terminal": {
    "path": "/opt/goby-test/exec-work-m3e/client-library-changed-source55-execution-06/failed-terminal.json",
    "sha256": "7ea63700f72924318e21e84c061fe1cda3f6d7f04373eaddf78a7f5abea1012d"
  },
  "token_sha256": "ca0338fefd06d4c5fb08797eecfad55f03e37731a8514fddc1482881d9a7c6dd",
  "user_id": "0dd576d477e8acea871cb4b06cb11153",
  "version": 1
});
const HISTORY_ENTRIES = Object.freeze([2, 3, 4, 5, 6].map(version => {
  const pins = version === 2 ? PRIOR_PINS : version === 3 ? HISTORY_V3_PINS : version === 4 ? HISTORY_V4_PINS : version === 5 ? HISTORY_V5_PINS : HISTORY_V6_PINS;
  return Object.freeze({ version, input: pins.prior_input, browser_report: pins.prior_browser_report,
    controller_report: pins.prior_controller_report, terminal: pins.prior_terminal,
    before_snapshot: pins.prior_before_snapshot, after_snapshot: pins.prior_after_snapshot });
}));
const PREVIOUS = Object.freeze({
  binary_sha256: 'cd67f2e71ff1b63e3c138cdba1f9c9d1e584e2788b47964a332b38311cda0e2d',
  process: Object.freeze({ pid: 1264063, start_ticks: 11104222, boot_id: BOOT }),
  runtime_sha256: 'd8689a4e0b36816ed462816856dfa73af6fba5f31f044632ed173db99c8842df',
  state_sha256: 'd6b8872eab70848b834c50e26cc2b84e9d137dd68049a1be85d09afa811829d4',
  server_id: SERVER, base_url: ORIGIN, direct_url: DIRECT,
  source: WORK + '/source-attempt-44',
  source_manifest_sha256: 'c2c9492589360b058c6533d0719219cf85ab5bc056bd76290cee51e7dc897f8b',
});
const PREVIOUS_PROCESS = Object.freeze({ ...PREVIOUS.process, executable: '/opt/goby-client-m3e/goby',
  sha256: PREVIOUS.binary_sha256, uid: 995, cgroup: '/system.slice/goby-client-m3e.service',
  invocation: 'c0a5244ae25646c8bd92c3e3c1636575' });
const PRIMARY = Object.freeze({ pid: 762090, start_ticks: 7637121, boot_id: BOOT,
  executable: '/opt/goby-dev/goby', sha256: 'af46a82e85fa67b776964a950ec85d12ca1c96ef94ce240f0287a8b8a009a620',
  uid: 995, cgroup: '/system.slice/goby-foundation-test.service', invocation: 'bb94d74b475f4382a6ec6f6df181dd74' });
const PROXY_PROCESS = Object.freeze({ pid: 334022, start_ticks: 378464, boot_id: BOOT });
const PINS = Object.freeze({
  proxy: { path: WORK + '/dual-proxy-status-02.json', sha256: '769684e6c84eb4ea83c01f86c671d94d488626581c8dde664fa6f7ab7c05310f' },
  catalog: { path: SOURCE + '/internal/backuppg/catalogs/schema-28-postgresql-17.json', sha256: CATALOG_SHA },
});
const FIXTURES = Object.freeze({
  profile_receipt: { path: WORK + '/client-special-features-protocol-finalization-v1/completed.json',
    sha256: 'b443e5f6d5faceb3486d68644298b0a1527f6dcc1e4ac1109ff6d0c252fdfb36' },
  profile_report: { path: WORK + '/client-special-features-protocol-finalization-v1/report.json',
    sha256: '929e6b6fc0014e6b3afc7ce023ea981b749ce884ed6eb33c34b3617ac116855d' },
  profile_inspection: { path: WORK + '/client-special-features-finalization-inspection-v1/report.json',
    sha256: 'fdc8a38f5937964d3dba8e0e47f897108475011e70f4802e30c1803068dc4b5e' },
  music_chain: { path: WORK + '/client-music-upgrade-chain-source32.json',
    sha256: 'c235d59a317fe1b95d8d78a51ab32ca556f3ca7872e96bfea51e710e26b0c67a' },
  music_scan_receipt: { path: WORK + '/client-music-scan-v1/receipt.json',
    sha256: 'e0b9ba686afb1950d4ab43c3ad5bc39d67090374704379103a29759c70aab93b' },
});
const SOURCES = Object.freeze(['client-browser-library-changed-source55.mjs', 'client-library-changed-source55-fixture.mjs',
  'client-browser-library-home.mjs', 'client-browser-cross-user.mjs', 'client-browser-session-proof.mjs',
  'client-browser-special-features-fixture.mjs', 'client-browser-goby-fixture.mjs']);
const LIBRARIES = Object.freeze([LIBRARY, '6383d20008836e137559698c29b10395', 'a34ce665fb75421ef7551570f353d705',
  '57a85c1ca5b6c7ae602c587755250b2f']);
const JSON_LIMIT = 2 * 1024 * 1024, SNAPSHOT_LIMIT = 64 * 1024 * 1024;
const SHA = /^[0-9a-f]{64}$/, ID = /^[0-9a-f]{32}$/;
const record = value => value !== null && typeof value === 'object' && !Array.isArray(value);
const digest = value => typeof value === 'string' && SHA.test(value) && !/^([0-9a-f])\1{63}$/.test(value);
const canonical = value => typeof value === 'bigint' ? value.toString() + 'n' : Array.isArray(value)
  ? '[' + value.map(canonical).join(',') + ']' : record(value)
    ? '{' + Object.keys(value).sort().map(key => JSON.stringify(key) + ':' + canonical(value[key])).join(',') + '}' : JSON.stringify(value);
const same = (left, right) => canonical(left) === canonical(right);
const exact = (value, keys) => record(value) && same(Object.keys(value).sort(), [...keys].sort());
const hash = value => createHash('sha256').update(value).digest('hex');
function need(value) {
  if (!value) {
    const error = new Error('library_changed_source55_binding_rejected');
    Error.captureStackTrace?.(error, need);
    throw error;
  }
}
const text = (value, maximum = 256) => typeof value === 'string' && value.length > 0 && value.length <= maximum && !/[\x00-\x1f\x7f]/.test(value);
const ownedPath = value => typeof value === 'string' && value.startsWith(WORK + '/') &&
  /^[\x21-\x7e]+$/.test(value) && !value.includes('\\') && path.posix.normalize(value) === value &&
  !value.split('/').some(part => part === '.' || part === '..');
const descriptor = value => exact(value, ['path', 'sha256']) && ownedPath(value.path) && digest(value.sha256);
function freeze(value) {
  if (value && typeof value === 'object') { for (const child of Object.values(value)) freeze(child); Object.freeze(value); }
  return value;
}

function historyScope(version) {
  need(version === 2 || version === 3 || version === 4 || version === 5 || version === 6);
  if (version === 2) return {
    version, root: PRIOR_ROOT, tool: PRIOR_TOOL, pins: PRIOR_PINS, terminalPins: PRIOR_TERMINAL_PINS,
    controllerUnit: PRIOR_CONTROLLER_UNIT, workerUnit: PRIOR_WORKER_UNIT,
    controllerInvocation: 'd6376750d8ff4fc08c97c69ac993f1e6', workerInvocation: '40884584c2784e3da2a91f816f563b45',
    beforeCounts: { sessions: 75, devices: 64, activity_entries: 167 }, afterCounts: { sessions: 76, devices: 65, activity_entries: 169 },
  };
  if (version === 3) return {
    version, root: HISTORY_V3_ROOT, tool: HISTORY_V3_TOOL, pins: HISTORY_V3_PINS, terminalPins: HISTORY_V3_TERMINAL_PINS,
    controllerUnit: 'goby-client-library-changed-ui-source55-controller-v3.service', workerUnit: 'goby-client-library-changed-ui-source55-v3.service',
    controllerInvocation: 'bb72fff9517e4f41baa01fbd3e86bf40', workerInvocation: 'e1296ab3a25a41d6964e577831df4152',
    beforeCounts: { sessions: 76, devices: 65, activity_entries: 169 }, afterCounts: { sessions: 77, devices: 66, activity_entries: 171 },
  };
  if (version === 4) return { version, root: HISTORY_V4_ROOT, tool: HISTORY_V4_TOOL, pins: HISTORY_V4_PINS, terminalPins: HISTORY_V4_TERMINAL_PINS,
    controllerUnit: 'goby-client-library-changed-ui-source55-controller-v4.service', workerUnit: 'goby-client-library-changed-ui-source55-v4.service',
    controllerInvocation: '6e2d992e43414cdc9aaf0f37be3ee688', workerInvocation: '8886fd825c644124ab82696979db2e73',
    beforeCounts: { sessions: 77, devices: 66, activity_entries: 171 }, afterCounts: { sessions: 78, devices: 67, activity_entries: 173 } };
  if (version === 5) return { version, root: HISTORY_V5_ROOT, tool: HISTORY_V5_TOOL, pins: HISTORY_V5_PINS, terminalPins: HISTORY_V5_TERMINAL_PINS,
    controllerUnit: 'goby-client-library-changed-ui-source55-controller-v5.service', workerUnit: 'goby-client-library-changed-ui-source55-v5.service',
    controllerInvocation: 'f47dfd37b99d435d95458d1401f215cd', workerInvocation: 'b8531eeb770e48559a5e3d42f2f4e6f0',
    beforeCounts: { sessions: 78, devices: 67, activity_entries: 173 }, afterCounts: { sessions: 79, devices: 68, activity_entries: 175 } };
  return { version, root: HISTORY_V6_ROOT, tool: HISTORY_V6_TOOL, pins: HISTORY_V6_PINS, terminalPins: HISTORY_V6_TERMINAL_PINS,
    controllerUnit: 'goby-client-library-changed-ui-source55-controller-v6.service', workerUnit: 'goby-client-library-changed-ui-source55-v6.service',
    controllerInvocation: 'aff7b1018c7542358bff7a52af59479d', workerInvocation: '04a5a5e520e54dad98bf3e551a88a706',
    beforeCounts: { sessions: 79, devices: 68, activity_entries: 175 }, afterCounts: { sessions: 81, devices: 69, activity_entries: 179 } };
}

function historicalControllerAuthority(version) {
  historyScope(version);
  if (version === 2) return { ...UPGRADE_PINS };
  if (version === 3) return { ...UPGRADE_PINS, ...PRIOR_PINS };
  return { ...UPGRADE_PINS, history: HISTORY_ENTRIES.slice(0, version === 4 ? 2 : version === 5 ? 3 : 4) };
}

function historicalInputAuthority(version) {
  const scope = historyScope(version);
  return { ...historicalControllerAuthority(version), before_snapshot: scope.pins.prior_before_snapshot };
}

const SETUP_PHASES = Object.freeze(['arguments', 'input', 'root_identity', 'process_identity', 'source_closure', 'baseline',
  'credentials', 'fixture', 'initial_pin', 'output', 'sources', 'authority', 'private_inputs']);
const STACK_GETTER = Object.getOwnPropertyDescriptor(new Error(), 'stack')?.get;

/** Return only bounded locations in this run's reviewed JavaScript closure. */
export function libraryChangedSetupDiagnostic(error, phase) {
  const selectedPhase = SETUP_PHASES.includes(phase) ? phase : 'arguments';
  const ownValue = (value, key) => {
    if (value === null || typeof value !== 'object') return undefined;
    const property = Object.getOwnPropertyDescriptor(value, key);
    return property && Object.hasOwn(property, 'value') ? property.value : undefined;
  };
  const safeFrames = value => {
    if (!record(value) || !same(Object.keys(value).sort(), ['frames', 'phase']) || !SETUP_PHASES.includes(ownValue(value, 'phase'))) return null;
    const frames = ownValue(value, 'frames');
    const length = ownValue(frames, 'length');
    if (!Array.isArray(frames) || !Number.isSafeInteger(length) || length < 0 || length > 3) return null;
    const result = [];
    for (let index = 0; index < length; index += 1) {
      const frame = ownValue(frames, String(index));
      if (!record(frame) || !same(Object.keys(frame).sort(), ['column', 'file', 'line'])) return null;
      const file = ownValue(frame, 'file'), line = ownValue(frame, 'line'), column = ownValue(frame, 'column');
      if (!SOURCES.includes(file) || !Number.isSafeInteger(line) || line < 1 || line > 10000000 ||
        !Number.isSafeInteger(column) || column < 1 || column > 10000000) return null;
      result.push({ file, line, column });
    }
    return { phase: ownValue(value, 'phase'), frames: result };
  };
  try {
    const preserved = safeFrames(ownValue(error, 'diagnostic'));
    if (preserved !== null) return freeze(preserved);
    const stackProperty = error !== null && typeof error === 'object' ? Object.getOwnPropertyDescriptor(error, 'stack') : undefined;
    let stack = ownValue(error, 'stack');
    // Only the exact accessor captured from this module's own Error may expose V8 stack data.
    if (stack === undefined && typeof STACK_GETTER === 'function' && stackProperty?.get === STACK_GETTER)
      stack = Reflect.apply(stackProperty.get, error, []);
    const frames = [];
    if (typeof stack === 'string' && stack.length <= 65536) {
      const prefix = TOOL + '/';
      for (const row of stack.split('\n').slice(1)) {
        if (!/^\s+at /.test(row)) continue;
        const matched = /(?:\(|\s)((?:file:\/\/)?\/[^\s()]+):(\d+):(\d+)\)?$/.exec(row);
        if (!matched) continue;
        const filename = matched[1].startsWith('file://') ? matched[1].slice('file://'.length) : matched[1];
        if (!filename.startsWith(prefix)) continue;
        const file = filename.slice(prefix.length), line = Number(matched[2]), column = Number(matched[3]);
        if (!SOURCES.includes(file) || !Number.isSafeInteger(line) || line < 1 || line > 10000000 ||
          !Number.isSafeInteger(column) || column < 1 || column > 10000000) continue;
        if (!frames.some(frame => frame.file === file && frame.line === line && frame.column === column)) frames.push({ file, line, column });
        if (frames.length === 3) break;
      }
    }
    return freeze({ phase: selectedPhase, frames });
  } catch { return freeze({ phase: selectedPhase, frames: [] }); }
}

function processIdentity(value) {
  return exact(value, ['pid', 'start_ticks', 'boot_id']) && Number.isSafeInteger(value.pid) && value.pid > 1 &&
    Number.isSafeInteger(value.start_ticks) && value.start_ticks > 0 && value.boot_id === BOOT;
}

function upgradeScope(authority) {
  need(record(authority) && descriptor(authority.upgrade_report));
  const prefix = WORK + '/client-schema28-source55-upgrade-';
  const filename = authority.upgrade_report.path;
  need(filename.startsWith(prefix) && filename.endsWith('/report.json'));
  const run = filename.slice(prefix.length, -'/report.json'.length);
  need(/^\d{8}_\d{6}_[0-9a-f]{12}$/.test(run));
  return { run, output: prefix + run, unit: 'goby-client-schema28-source55-' + run.replaceAll('_', '-') + '.service' };
}

export function validateLibraryChangedSource55Input(input) {
  need(exact(input, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture',
    'expected_libraries', 'target', 'source_closure', 'authority', 'controller']) &&
    input.marker === 'goby-client-library-changed-input-v1' && input.version === 1 &&
    input.mode === 'b-movies-name-automatic-refresh' && input.root === ROOT && input.output === ROOT + '/browser');
  const candidate = input.candidate;
  need(exact(candidate, ['binary_sha256', 'process', 'runtime_sha256', 'state_sha256', 'server_id', 'base_url', 'direct_url',
    'source', 'source_manifest_sha256', 'invocation_id', 'publication']) &&
    ['binary_sha256', 'runtime_sha256', 'state_sha256'].every(key => digest(candidate[key]) && candidate[key] !== PREVIOUS[key]) &&
    candidate.binary_sha256 !== PRIMARY.sha256 && processIdentity(candidate.process) &&
    ![PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(candidate.process.pid) &&
    candidate.process.start_ticks > PREVIOUS.process.start_ticks && candidate.server_id === SERVER &&
    candidate.base_url === ORIGIN && candidate.direct_url === DIRECT && candidate.source === SOURCE &&
    candidate.source_manifest_sha256 === SOURCE_SHA && typeof candidate.invocation_id === 'string' && ID.test(candidate.invocation_id) &&
    !/^([0-9a-f])\1{31}$/.test(candidate.invocation_id) && ![PREVIOUS_PROCESS.invocation, PRIMARY.invocation].includes(candidate.invocation_id) &&
    typeof candidate.publication === 'string' && /^[0-9a-f]{40}$/.test(candidate.publication) &&
    !/^([0-9a-f])\1{39}$/.test(candidate.publication) && candidate.publication !== '35ae3d000f812fa18d234921cedb3c33d88190e0');
  need(same(input.fixture, FIXTURES));
  need(exact(input.actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) &&
    input.actor.slot === 'B' && input.actor.user_id === USER && input.actor.account_key === 'viewer' &&
    input.actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(input.actor.credentials) &&
    input.actor.credentials.path === ROOT + '/viewer-credentials.json');
  const authority = input.authority;
  need(exact(authority, [...Object.keys(UPGRADE_PINS), 'history', 'before_snapshot']) && descriptor(authority.before_snapshot) &&
    Object.entries(UPGRADE_PINS).every(([key, value]) => same(authority[key], value)) && Array.isArray(authority.history) && authority.history.length === 5 &&
    authority.history.every((entry, index) => exact(entry, ['version', 'input', 'browser_report', 'controller_report', 'terminal', 'before_snapshot', 'after_snapshot']) &&
      entry.version === index + 2 && ['input', 'browser_report', 'controller_report', 'terminal', 'before_snapshot', 'after_snapshot'].every(key => descriptor(entry[key])) &&
      same(entry, HISTORY_ENTRIES[index])));
  const scope = upgradeScope(authority);
  need(authority.upgrade_intent.path === UPGRADE_TOOL + '/intent.json' &&
    authority.upgrade_attestation.path === scope.output + '/attestation.json' &&
    authority.current_snapshot.path === scope.output + '/after-full.json' &&
    authority.before_snapshot.path === ROOT + '/before-full.json' &&
    authority.current_snapshot.sha256 !== '738eb9717506bec997456830e70d56f0867f9cf8159939ea3733e039cadb9811');
  need(exact(input.source_closure, SOURCES.map(name => TOOL + '/' + name)) && Object.values(input.source_closure).every(digest));
  need(Array.isArray(input.expected_libraries) && input.expected_libraries.length === 4 &&
    input.expected_libraries.every(value => exact(value, ['id', 'name']) && typeof value.id === 'string' && ID.test(value.id) && text(value.name)) &&
    same(input.expected_libraries.map(value => value.id).sort(), [...LIBRARIES].sort()) &&
    input.expected_libraries.find(value => value.id === LIBRARY)?.name === 'M3e Client Movies');
  const target = input.target;
  need(exact(target, ['id', 'library_id', 'name', 'type', 'parent_id', 'root_id', 'relative_path']) &&
    target.id === ITEM && target.library_id === LIBRARY && target.type === 'Movie' && text(target.name) &&
    typeof target.parent_id === 'string' && ID.test(target.parent_id) && typeof target.root_id === 'string' && ID.test(target.root_id) &&
    text(target.relative_path, 4096) &&
    !target.relative_path.startsWith('/') && !target.relative_path.includes('\\') &&
    !target.relative_path.split('/').some(part => part === '' || part === '.' || part === '..'));
  const controller = input.controller;
  need(exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    ![candidate.process.pid, PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(controller.pid) &&
    typeof controller.start_ticks === 'string' && /^[1-9]\d*$/.test(controller.start_ticks) &&
    Number.isSafeInteger(Number(controller.start_ticks)) && controller.boot_id === BOOT && controller.unit === CONTROLLER_UNIT);
  return true;
}
export function validateLibraryChangedSource55Viewer(value) {
  need(exact(value, ['marker', 'slot', 'user_id', 'base_url', 'direct_url', 'viewer']) &&
    value.marker === 'goby-client-library-changed-viewer-v1' && value.slot === 'B' && value.user_id === USER &&
    value.base_url === ORIGIN && value.direct_url === DIRECT && exact(value.viewer, ['username', 'password']) &&
    value.viewer.username === 'm3e-client-viewer' && typeof value.viewer.password === 'string' && /^[0-9a-f]{48}$/.test(value.viewer.password));
  return true;
}

function sourceBinding(value, publication) {
  need(exact(value, ['marker', 'schema', 'source', 'source_manifest_sha256', 'catalog_sha256', 'publication']) &&
    value.marker === 'goby-client-schema28-source-v1' && value.schema === 28 && value.source === SOURCE &&
    value.source_manifest_sha256 === SOURCE_SHA && value.catalog_sha256 === CATALOG_SHA && value.publication === publication);
}

function catalogColumns(catalog) {
  need(record(catalog) && record(catalog.catalog) && Array.isArray(catalog.catalog.Tables) && catalog.catalog.Tables.length === 35 &&
    Array.isArray(catalog.objects));
  const relations = new Map(), columnFacts = new Map();
  for (const row of catalog.objects.filter(value => value?.kind === 'relation')) {
    need(record(row) && typeof row.name === 'string' && /^[a-z][a-z0-9_]*$/.test(row.name) && !relations.has(row.name) &&
      record(row.value) && ['r', 'i', 'S'].includes(row.value.kind));
    relations.set(row.name, row.value.kind);
  }
  for (const row of catalog.objects.filter(value => value?.kind === 'column')) {
    const matched = typeof row.name === 'string' ? /^([a-z][a-z0-9_]*)\.([0-9]{5})$/.exec(row.name) : null;
    need(matched && Number(matched[2]) > 0 && relations.has(matched[1]) && !columnFacts.has(row.name) &&
      record(row.value) && typeof row.value.dropped === 'boolean');
    // Index and sequence columns remain catalog evidence but are not table row projections.
    columnFacts.set(row.name, { parent: matched[1], kind: relations.get(matched[1]), fact: row });
  }
  const result = Object.create(null);
  for (const table of catalog.catalog.Tables) {
    need(record(table) && typeof table.Name === 'string' && /^[a-z][a-z0-9_]*$/.test(table.Name) && !Object.hasOwn(result, table.Name) &&
      Array.isArray(table.Columns));
    const prefix = table.Name + '.';
    need(relations.get(table.Name) === 'r');
    const facts = [...columnFacts.values()].filter(value => value.parent === table.Name && value.kind === 'r' &&
      value.fact.value.dropped === false).map(value => value.fact).sort((left, right) => left.name.localeCompare(right.name));
    // Full ordinal catalog facts include generated columns omitted by restore projections.
    need(facts.length > 0 && facts.every(row => /^\d{5}$/.test(row.name.slice(prefix.length))) &&
      new Set(facts.map(row => row.name)).size === facts.length);
    const columns = facts.map(row => row.value.name);
    need(columns.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) &&
      new Set(columns).size === columns.length && table.Columns.every(value => columns.includes(value)));
    result[table.Name] = columns;
  }
  need([...columnFacts.values()].every(value => value.kind === 'r' ? Object.hasOwn(result, value.parent) : ['i', 'S'].includes(value.kind)));
  need(same([...relations.entries()].filter(([, kind]) => kind === 'r').map(([name]) => name).sort(),
    Object.keys(result).sort()));
  return result;
}

function catalogSequences(catalog, database) {
  const sequences = catalog.catalog.Sequences, facts = catalog.objects.filter(row => row?.kind === 'sequence');
  need(Array.isArray(sequences) && sequences.length === 5 && facts.length === 5 && record(database.sequences));
  const names = sequences.map(value => value.Name);
  need(names.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) && new Set(names).size === 5 &&
    same(Object.keys(database.sequences).sort(), [...names].sort()) && same(facts.map(value => value.name).sort(), [...names].sort()) &&
    same(catalog.objects.filter(row => row?.kind === 'relation' && row.value?.kind === 'S').map(row => row.name).sort(), [...names].sort()));
  const integral = value => typeof value === 'bigint' || Number.isSafeInteger(value);
  for (const sequence of sequences) {
    const definition = facts.find(value => value.name === sequence.Name).value, value = database.sequences[sequence.Name];
    need(record(definition) && definition.owned_by === sequence.Table + '.' + sequence.Column &&
      same(definition.min, sequence.MinValue) && same(definition.max, sequence.MaxValue) && same(definition.increment, sequence.Increment) &&
      [sequence.MinValue, sequence.MaxValue, sequence.Increment].every(integral) && sequence.Increment > 0 &&
      Array.isArray(sequence.Consumers) && sequence.Consumers.length > 0 &&
      exact(value, ['last_value', 'is_called']) && integral(value.last_value) && typeof value.is_called === 'boolean');
    const last = BigInt(value.last_value), minimum = BigInt(sequence.MinValue), maximum = BigInt(sequence.MaxValue);
    const next = last + (value.is_called ? BigInt(sequence.Increment) : 0n);
    need(last >= minimum && last <= maximum && next <= maximum && sequence.Consumers.every(consumer =>
      exact(consumer, ['Table', 'Column']) && Array.isArray(database.tables[consumer.Table]) &&
      database.tables[consumer.Table].every(row => integral(row[consumer.Column]) && BigInt(row[consumer.Column]) < next)));
  }
}

function validateSchema28Snapshots(current, before, catalog, candidate) {
  need(current?.schema === 28 && before?.schema === 28 && current.runtime_sha256 === candidate.runtime_sha256 &&
    current.browser_sha256 === SOURCE_CREDENTIALS_SHA && record(current.database) && record(before.database) &&
    record(catalog) && catalog.version === 28 && catalog.postgresql_major === 17 && Array.isArray(catalog.objects) &&
    same(current.database.catalog, catalog.objects) && current.database.unsupported === false);
  const withoutTime = snapshot => {
    need(record(snapshot.database.metadata) && typeof snapshot.database.metadata.captured_at === 'string' &&
      Number.isFinite(Date.parse(snapshot.database.metadata.captured_at)));
    const metadata = { ...snapshot.database.metadata }; delete metadata.captured_at;
    return { ...snapshot, database: { ...snapshot.database, metadata } };
  };
  need(same(withoutTime(current), withoutTime(before)) &&
    Date.parse(before.database.metadata.captured_at) > Date.parse(current.database.metadata.captured_at));
  const tables = current.database.tables, columns = current.database.metadata.columns, trustedColumns = catalogColumns(catalog);
  need(record(tables) && Object.keys(tables).length === 35 && record(columns) && same(columns, trustedColumns) &&
    same(Object.keys(columns).sort(), Object.keys(tables).sort()));
  for (const [name, rows] of Object.entries(tables)) {
    const projection = columns[name];
    need(Array.isArray(rows) && Array.isArray(projection) && projection.length > 0 &&
      projection.every(value => typeof value === 'string' && /^[a-z][a-z0-9_]*$/.test(value)) &&
      new Set(projection).size === projection.length && rows.every(row => exact(row, projection)));
  }
  catalogSequences(catalog, current.database);
  need(tables.library_roots?.length === 4 && tables.libraries?.length === 4 && tables.items?.length === 22 &&
    tables.play_sessions?.length === 26 && tables.user_item_data?.length === 7 &&
    tables.sessions?.length === 75 && tables.devices?.length === 64 && tables.activity_entries?.length === 167);
  need(same(columns.library_roots.slice(-4), ['binding_revision', 'storage_binding', 'bound_at', 'bound_by']) &&
    same(columns.activity_entries.slice(-2), ['previous_revision', 'observation_fingerprint']) &&
    tables.library_roots.every(row => row.binding_revision === 1 && row.storage_binding === null &&
      row.bound_at === null && row.bound_by === null && row.relative_path === '.' && row.allowed_path === row.path) &&
    tables.activity_entries.every(row => row.previous_revision === 0 && row.observation_fingerprint === ''));
  need(Array.isArray(catalog.migrations) && catalog.migrations.length === 28 &&
    Array.isArray(tables.schema_migrations) && tables.schema_migrations.length === 28 &&
    same([...tables.schema_migrations].sort((left, right) => left.version - right.version).map(row => ({ version: row.version, name: row.name })),
      catalog.migrations.map(row => ({ version: row.version, name: row.name }))));
  return tables;
}

function ledgerInstant(value) {
  const matched = typeof value === 'string' ? /^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})(?:\.(\d{1,6}))?(?:Z|\+00:00)$/.exec(value) : null;
  need(matched);
  const whole = Date.parse(matched[1] + 'Z');
  need(Number.isSafeInteger(whole) && new Date(whole).toISOString().slice(0, 19) === matched[1]);
  return BigInt(whole) * 1000n + BigInt((matched[2] ?? '').padEnd(6, '0'));
}

function identicalSnapshotWithLaterCapture(left, right) {
  need(record(left) && record(right) && record(left.database?.metadata) && record(right.database?.metadata));
  const clean = value => {
    const metadata = { ...value.database.metadata }; delete metadata.captured_at;
    return { ...value, database: { ...value.database, metadata } };
  };
  need(same(clean(left), clean(right)) && ledgerInstant(right.database.metadata.captured_at) > ledgerInstant(left.database.metadata.captured_at));
}

function validatePriorLogin(proof) {
  need(exact(proof, ['token_sha256', 'session_id', 'user_id', 'client_name', 'device_id', 'device_name', 'client_version',
    'server_id', 'created_at', 'created_at_source', 'kind', 'slot', 'frame_login_finished', 'physical_login_completed', 'request_metadata_matches']) &&
    digest(proof.token_sha256) && typeof proof.session_id === 'string' && ID.test(proof.session_id) &&
    proof.user_id === USER && proof.server_id === SERVER && proof.kind === 'emby' && proof.slot === 'B' &&
    proof.created_at_source === 'SessionInfo.LastActivityDate' &&
    ['frame_login_finished', 'physical_login_completed', 'request_metadata_matches'].every(key => proof[key] === true) &&
    ['client_name', 'device_id', 'device_name', 'client_version'].every(key => text(proof[key], 256)));
  ledgerInstant(proof.created_at);
}

function validatePriorSnapshotDelta(before, after, browser, version = 2) {
  const scope = historyScope(version);
  need(scope.version !== 6);
  const proof = browser.login_proof; validatePriorLogin(proof);
  need(record(before) && record(after) && before.schema === 28 && after.schema === 28 &&
    same(Object.keys(before).sort(), Object.keys(after).sort()) &&
    Object.keys(before).filter(key => key !== 'database').every(key => same(before[key], after[key])));
  const left = before.database, right = after.database;
  need(record(left) && record(right) && same(Object.keys(left).sort(), Object.keys(right).sort()) &&
    Object.keys(left).filter(key => !['tables', 'sequences', 'metadata'].includes(key)).every(key => same(left[key], right[key])) &&
    record(left.metadata) && record(right.metadata) && same(Object.keys(left.metadata).sort(), Object.keys(right.metadata).sort()) &&
    Object.keys(left.metadata).filter(key => key !== 'captured_at').every(key => same(left.metadata[key], right.metadata[key])) &&
    record(left.tables) && record(right.tables) && Object.keys(left.tables).length === 35 &&
    same(Object.keys(left.tables).sort(), Object.keys(right.tables).sort()));
  const start = ledgerInstant(left.metadata.captured_at), end = ledgerInstant(right.metadata.captured_at);
  need(start < end && ledgerInstant(browser.started_at) >= start && ledgerInstant(browser.completed_at) <= end &&
    ledgerInstant(browser.started_at) <= ledgerInstant(browser.completed_at));
  const additions = Object.create(null);
  for (const [name, rows] of Object.entries(left.tables)) {
    const current = right.tables[name], columns = left.metadata.columns[name];
    need(Array.isArray(rows) && Array.isArray(current) && Array.isArray(columns) &&
      [...rows, ...current].every(row => exact(row, columns)));
    if (!['sessions', 'devices', 'activity_entries'].includes(name)) {
      need(same(rows, current)); continue;
    }
    const remaining = new Map();
    for (const row of rows) { const key = canonical(row); remaining.set(key, (remaining.get(key) ?? 0) + 1); }
    const added = [];
    for (const row of current) {
      const key = canonical(row), count = remaining.get(key) ?? 0;
      if (count > 0) remaining.set(key, count - 1); else added.push(row);
    }
    need([...remaining.values()].every(value => value === 0) && new Set(added.map(canonical)).size === added.length);
    additions[name] = added;
  }
  need(Object.entries(scope.beforeCounts).every(([name, count]) => left.tables[name].length === count) &&
    Object.entries(scope.afterCounts).every(([name, count]) => right.tables[name].length === count) &&
    additions.sessions.length === 1 && additions.devices.length === 1 && additions.activity_entries.length === 2);
  const session = additions.sessions[0], device = additions.devices[0];
  const integral = value => typeof value === 'bigint' || Number.isSafeInteger(value);
  need(left.tables.sessions.every(row => row.id !== proof.session_id && row.token_hash !== '\\x' + proof.token_sha256) &&
    session.id === proof.session_id && session.token_hash === '\\x' + proof.token_sha256 && session.user_id === USER && session.kind === 'emby' &&
    session.device_id === proof.device_id && ['client_name', 'device_name', 'client_version'].every(key => session[key] === proof[key]) &&
    ledgerInstant(session.created_at) === ledgerInstant(proof.created_at) && record(session.client_capabilities) && integral(session.device_registry_id));
  const issued = ledgerInstant(session.created_at), touched = ledgerInstant(session.last_seen_at), revoked = ledgerInstant(session.revoked_at);
  need(start <= issued && issued <= touched && touched <= end && issued <= revoked && revoked <= end &&
    ledgerInstant(browser.started_at) <= issued && revoked <= ledgerInstant(browser.completed_at) &&
    ledgerInstant(session.expires_at) === issued + 30n * 24n * 60n * 60n * 1000000n);
  need(integral(device.id) && same(device.id, session.device_registry_id) &&
    left.tables.devices.every(row => row.id !== device.id && row.reported_device_id !== proof.device_id) &&
    device.reported_device_id === proof.device_id && device.reported_name === proof.device_name && device.app_name === proof.client_name &&
    device.app_version === proof.client_version && device.last_user_id === USER && device.ip_address === '127.0.0.1' &&
    device.custom_name === null && device.deleted_at === null && device.revision === 1 &&
    start <= ledgerInstant(device.created_at) && ledgerInstant(device.created_at) <= issued &&
    ledgerInstant(device.created_at) <= ledgerInstant(device.last_seen_at) && ledgerInstant(device.last_seen_at) <= end);
  const audits = additions.activity_entries;
  need(same(audits.map(row => row.action).sort(), ['session.login', 'session.revoked']) &&
    audits.every(row => integral(row.id) && row.severity === 'Info' && row.source === 'emby' && row.actor_kind === 'user' &&
      row.actor_id === USER && row.actor_credential_id === session.id && row.resource_kind === 'session' && row.resource_id === session.id &&
      row.revision === 0 && row.affected_count === 1 && row.request_id === '' && row.state === '' && same(row.changed_fields, []) &&
      row.previous_revision === 0 && row.observation_fingerprint === '' && start <= ledgerInstant(row.created_at) && ledgerInstant(row.created_at) <= end));
  need(record(left.sequences) && record(right.sequences) && same(Object.keys(left.sequences).sort(), Object.keys(right.sequences).sort()));
  for (const [name, initial] of Object.entries(left.sequences)) {
    const current = right.sequences[name], table = { devices_id_seq: 'devices', activity_entries_id_seq: 'activity_entries' }[name];
    need(exact(initial, ['last_value', 'is_called']) && exact(current, ['last_value', 'is_called']) &&
      integral(initial.last_value) && integral(current.last_value) && typeof initial.is_called === 'boolean' && typeof current.is_called === 'boolean');
    if (!table) { need(same(initial, current)); continue; }
    const added = additions[table], first = BigInt(initial.last_value) + 1n;
    need(initial.is_called === true && current.is_called === true && BigInt(current.last_value) === BigInt(initial.last_value) + BigInt(added.length) &&
      same(added.map(row => BigInt(row.id)).sort((a, b) => a < b ? -1 : a > b ? 1 : 0), added.map((_, index) => first + BigInt(index))));
  }
  need(Object.hasOwn(left.sequences, 'devices_id_seq') && Object.hasOwn(left.sequences, 'activity_entries_id_seq'));
  return { new_sessions: 1, new_devices: 1, new_audits: 2, metadata_revision_delta: 0,
    old_rows_sequences_private_preserved: true, owned_sessions_closed: true };
}

function validateHistorySixSnapshotDelta(before, after, browser, identity) {
  const scope = historyScope(6);
  const proof = browser.login_proof; validatePriorLogin(proof);
  need(record(before) && record(after) && before.schema === 28 && after.schema === 28 &&
    same(Object.keys(before).sort(), Object.keys(after).sort()) &&
    Object.keys(before).filter(key => key !== 'database').every(key => same(before[key], after[key])));
  const left = before.database, right = after.database;
  need(record(left) && record(right) && same(Object.keys(left).sort(), Object.keys(right).sort()) &&
    Object.keys(left).filter(key => !['tables', 'sequences', 'metadata'].includes(key)).every(key => same(left[key], right[key])) &&
    record(left.metadata) && record(right.metadata) && same(Object.keys(left.metadata).sort(), Object.keys(right.metadata).sort()) &&
    Object.keys(left.metadata).filter(key => key !== 'captured_at').every(key => same(left.metadata[key], right.metadata[key])) &&
    record(left.tables) && record(right.tables) && Object.keys(left.tables).length === 35 &&
    same(Object.keys(left.tables).sort(), Object.keys(right.tables).sort()));
  const start = ledgerInstant(left.metadata.captured_at), end = ledgerInstant(right.metadata.captured_at);
  need(start < end && ledgerInstant(browser.started_at) >= start && ledgerInstant(browser.completed_at) <= end &&
    ledgerInstant(browser.started_at) <= ledgerInstant(browser.completed_at));
  const additions = Object.create(null);
  for (const [name, rows] of Object.entries(left.tables)) {
    const current = right.tables[name], columns = left.metadata.columns[name];
    need(Array.isArray(rows) && Array.isArray(current) && Array.isArray(columns) &&
      [...rows, ...current].every(row => exact(row, columns)));
    if (!['sessions', 'devices', 'activity_entries'].includes(name)) {
      need(same(rows, current)); continue;
    }
    const remaining = new Map();
    for (const row of rows) { const key = canonical(row); remaining.set(key, (remaining.get(key) ?? 0) + 1); }
    const added = [];
    for (const row of current) {
      const key = canonical(row), count = remaining.get(key) ?? 0;
      if (count > 0) remaining.set(key, count - 1); else added.push(row);
    }
    need([...remaining.values()].every(value => value === 0) && new Set(added.map(canonical)).size === added.length);
    additions[name] = added;
  }
  need(Object.entries(scope.beforeCounts).every(([name, count]) => left.tables[name].length === count) &&
    Object.entries(scope.afterCounts).every(([name, count]) => right.tables[name].length === count) &&
    additions.sessions.length === 2 && additions.devices.length === 1 && additions.activity_entries.length === 4);
  const session = additions.sessions.find(row => row.id === proof.session_id), device = additions.devices[0];
  need(session);
  const integral = value => typeof value === 'bigint' || Number.isSafeInteger(value);
  need(left.tables.sessions.every(row => row.id !== proof.session_id && row.token_hash !== '\\x' + proof.token_sha256) &&
    session.id === proof.session_id && session.token_hash === '\\x' + proof.token_sha256 && session.user_id === USER && session.kind === 'emby' &&
    session.device_id === proof.device_id && ['client_name', 'device_name', 'client_version'].every(key => session[key] === proof[key]) &&
    ledgerInstant(session.created_at) === ledgerInstant(proof.created_at) && record(session.client_capabilities) && integral(session.device_registry_id));
  const issued = ledgerInstant(session.created_at), touched = ledgerInstant(session.last_seen_at), revoked = ledgerInstant(session.revoked_at);
  need(start <= issued && issued <= touched && touched <= end && issued <= revoked && revoked <= end &&
    ledgerInstant(browser.started_at) <= issued && revoked <= ledgerInstant(browser.completed_at) &&
    ledgerInstant(session.expires_at) === issued + 30n * 24n * 60n * 60n * 1000000n);
  need(integral(device.id) && same(device.id, session.device_registry_id) &&
    left.tables.devices.every(row => row.id !== device.id && row.reported_device_id !== proof.device_id) &&
    device.reported_device_id === proof.device_id && device.reported_name === proof.device_name && device.app_name === proof.client_name &&
    device.app_version === proof.client_version && device.last_user_id === USER && device.ip_address === '127.0.0.1' &&
    device.custom_name === null && device.deleted_at === null && device.revision === 1 &&
    start <= ledgerInstant(device.created_at) && ledgerInstant(device.created_at) <= issued &&
    ledgerInstant(device.created_at) <= ledgerInstant(device.last_seen_at) && ledgerInstant(device.last_seen_at) <= end);
  const audits = additions.activity_entries.filter(row => row.resource_id === session.id);
  need(audits.length === 2 && same(audits.map(row => row.action).sort(), ['session.login', 'session.revoked']) &&
    audits.every(row => integral(row.id) && row.severity === 'Info' && row.source === 'emby' && row.actor_kind === 'user' &&
      row.actor_id === USER && row.actor_credential_id === session.id && row.resource_kind === 'session' && row.resource_id === session.id &&
      row.revision === 0 && row.affected_count === 1 && row.request_id === '' && row.state === '' && same(row.changed_fields, []) &&
      row.previous_revision === 0 && row.observation_fingerprint === '' && start <= ledgerInstant(row.created_at) && ledgerInstant(row.created_at) <= end));
  need(exact(identity, ['session_id', 'token_sha256', 'user_id', 'kind']) && ID.test(identity.session_id) && digest(identity.token_sha256) &&
    identity.user_id === '0dd576d477e8acea871cb4b06cb11153' && identity.kind === 'admin');
  const administrator = additions.sessions.find(row => row.id === identity.session_id);
  need(administrator && administrator !== session && administrator.token_hash === '\\x' + identity.token_sha256 &&
    left.tables.sessions.every(row => row.id !== administrator.id && row.token_hash !== administrator.token_hash) &&
    administrator.token_hash !== session.token_hash && administrator.user_id === identity.user_id && administrator.kind === 'admin' &&
    administrator.client_name === 'Goby Dashboard' && administrator.device_id === 'goby-dashboard' && administrator.device_name === 'Web browser' &&
    administrator.device_registry_id === null && same(administrator.client_capabilities, {}) && administrator.last_seen_at === administrator.created_at &&
    left.tables.users.filter(row => row.id === identity.user_id && row.is_administrator === true && row.is_disabled === false).length === 1);
  const nativeIssued = ledgerInstant(administrator.created_at), nativeRevoked = ledgerInstant(administrator.revoked_at);
  need(start <= nativeIssued && nativeIssued <= nativeRevoked && nativeRevoked <= end &&
    ledgerInstant(administrator.expires_at) === nativeIssued + 24n * 60n * 60n * 1000000n);
  const nativeAudits = additions.activity_entries.filter(row => row.resource_id === administrator.id);
  need(nativeAudits.length === 2 && same(nativeAudits.map(row => row.action).sort(), ['session.login', 'session.revoked']) &&
    nativeAudits.every(row => integral(row.id) && row.severity === 'Info' && row.source === 'native' && row.actor_kind === 'user' &&
      row.actor_id === administrator.user_id && row.actor_credential_id === administrator.id && row.resource_kind === 'session' && row.resource_id === administrator.id &&
      row.revision === 0 && row.affected_count === 1 && row.request_id === '' && row.state === '' && same(row.changed_fields, []) &&
      row.previous_revision === 0 && row.observation_fingerprint === '' && nativeIssued <= ledgerInstant(row.created_at) && ledgerInstant(row.created_at) <= end));
  need(record(left.sequences) && record(right.sequences) && same(Object.keys(left.sequences).sort(), Object.keys(right.sequences).sort()));
  for (const [name, initial] of Object.entries(left.sequences)) {
    const current = right.sequences[name], table = { devices_id_seq: 'devices', activity_entries_id_seq: 'activity_entries' }[name];
    need(exact(initial, ['last_value', 'is_called']) && exact(current, ['last_value', 'is_called']) &&
      integral(initial.last_value) && integral(current.last_value) && typeof initial.is_called === 'boolean' && typeof current.is_called === 'boolean');
    if (!table) { need(same(initial, current)); continue; }
    const added = additions[table], first = BigInt(initial.last_value) + 1n;
    need(initial.is_called === true && current.is_called === true && BigInt(current.last_value) === BigInt(initial.last_value) + BigInt(added.length) &&
      same(added.map(row => BigInt(row.id)).sort((a, b) => a < b ? -1 : a > b ? 1 : 0), added.map((_, index) => first + BigInt(index))));
  }
  need(Object.hasOwn(left.sequences, 'devices_id_seq') && Object.hasOwn(left.sequences, 'activity_entries_id_seq'));
  return { new_sessions: 2, new_devices: 1, new_audits: 4, metadata_revision_delta: 0,
    old_rows_sequences_private_preserved: true, owned_sessions_closed: true };
}

function validatePriorClosure(value) {
  need(exact(value, ['context_closed', 'browser_closed', 'proxy_closed', 'http_pending', 'websocket_pending', 'websocket_active',
    'websocket_opened', 'websocket_closed', 'sockets_remaining', 'cleanup_failures']) &&
    ['context_closed', 'browser_closed', 'proxy_closed'].every(key => value[key] === true) &&
    ['http_pending', 'websocket_pending', 'websocket_active', 'sockets_remaining'].every(key => value[key] === 0) &&
    value.websocket_opened === 1 && value.websocket_closed === 1 && same(value.cleanup_failures, []));
}

function validatePriorTerminal(input, terminal, priorInput, browser, report, ledger, scope) {
  need(exact(terminal, ['automatic_retry', 'before_snapshot', 'browser', 'browser_report', 'candidate', 'candidate_preserved', 'captured_at',
    'cleanup', 'cleanup_needed', 'cleanup_performed', 'client_acceptance', 'controller_source', 'current_matches_prior_after',
    'dispatched_native_intents', 'exact_owned_additions_retained', 'failed_units', 'full_m3_complete', 'http_requests', 'independent_snapshot',
    'input', 'ledger', 'library_changed_client_acceptance', 'marker', 'media_fact_sha256', 'media_preserved', 'observed_run_status',
    'old_rows_sequences_private_preserved', 'old_v1_scope_preserved', 'owned_session_closed', 'phase', 'primary_fact_sha256',
    'primary_invocation_id', 'primary_preserved', 'primary_process', 'prior_after_snapshot', 'report', 'reserved_native_intents',
    'restoration', 'schema', 'scope', 'scope_files', 'scope_files_unchanged', 'seal_script', 'service_writes', 'sql_business_writes',
    'status', 'tool', 'upgrade_authority_snapshot', 'version', ...(scope.version === 3 ? ['baseline_chain_verified', 'cumulative_totals',
      'discovery_failure', 'old_v2_scope_preserved', 'predecessor_inventories', 'predecessor_terminals', 'prior_baseline', 'prior_failure_preservation']
      : scope.version === 4 || scope.version === 5 ? ['baseline_chain_verified', 'cumulative_totals', 'discovery_failure', 'discovery_screenshot', 'discovery_screenshot_bytes',
        'history_preservation', 'old_v2_scope_preserved', 'old_v3_scope_preserved', 'predecessor_inventories', 'predecessor_terminals',
        'predecessor_units_preserved', 'prior_baseline', ...(scope.version === 5 ? ['old_v4_scope_preserved', 'guard_evidence_preserved', 'guard_evidence'] : [])] : [])]) &&
    terminal.marker === 'goby-source55-failed-ui-terminal-v' + scope.version && terminal.version === 1 && terminal.status === 'failed_scope_sealed' &&
    terminal.observed_run_status === 'failed' && terminal.phase === 'discovery' && terminal.schema === 28 &&
    terminal.scope === scope.root && terminal.tool === scope.tool && terminal.cleanup === 'not_required' && terminal.restoration === 'not_required' &&
    terminal.http_requests === 0 && terminal.service_writes === 0 && typeof terminal.captured_at === 'string' && Number.isFinite(Date.parse(terminal.captured_at)) &&
    ['candidate_preserved', 'current_matches_prior_after', 'exact_owned_additions_retained', 'media_preserved', 'old_rows_sequences_private_preserved',
      'old_v1_scope_preserved', 'owned_session_closed', 'primary_preserved', 'scope_files_unchanged'].every(key => terminal[key] === true) &&
    ['automatic_retry', 'cleanup_needed', 'cleanup_performed', 'client_acceptance', 'full_m3_complete', 'library_changed_client_acceptance',
      'sql_business_writes'].every(key => terminal[key] === false) && same(terminal.reserved_native_intents, []) &&
    same(terminal.dispatched_native_intents, []) && same(terminal.ledger, ledger));
  for (const [key, expected] of Object.entries({ input: scope.pins.prior_input, report: scope.pins.prior_controller_report,
    browser_report: scope.pins.prior_browser_report, before_snapshot: scope.pins.prior_before_snapshot,
    prior_after_snapshot: scope.pins.prior_after_snapshot, upgrade_authority_snapshot: UPGRADE_PINS.current_snapshot, ...scope.terminalPins }))
    need(same(terminal[key], expected));
  if (scope.version === 3) need(terminal.baseline_chain_verified === true && terminal.old_v2_scope_preserved === true &&
    same(terminal.cumulative_totals, scope.afterCounts) && same(terminal.prior_failure_preservation, ledger) &&
    same(terminal.prior_baseline, PRIOR_TERMINAL_PINS.independent_snapshot) &&
    Object.entries(HISTORY_V3_PREDECESSORS).every(([key, value]) => same(terminal[key], value)));
  if (scope.version === 4) need(terminal.baseline_chain_verified === true && terminal.old_v2_scope_preserved === true &&
    terminal.old_v3_scope_preserved === true && terminal.predecessor_units_preserved === true &&
    same(terminal.cumulative_totals, scope.afterCounts) && same(terminal.history_preservation, report.history_preservation) &&
    same(terminal.prior_baseline, HISTORY_V3_TERMINAL_PINS.independent_snapshot) && terminal.discovery_screenshot_bytes === 38695 &&
    Object.entries(HISTORY_V4_PREDECESSORS).every(([key, value]) => same(terminal[key], value)));
  if (scope.version === 5) need(terminal.baseline_chain_verified === true && terminal.old_v2_scope_preserved === true &&
    terminal.old_v3_scope_preserved === true && terminal.old_v4_scope_preserved === true && terminal.predecessor_units_preserved === true &&
    terminal.guard_evidence_preserved === true && same(terminal.guard_evidence, HISTORY_V5_GUARD_EVIDENCE) &&
    same(terminal.cumulative_totals, scope.afterCounts) && same(terminal.history_preservation, report.history_preservation) &&
    same(terminal.prior_baseline, HISTORY_V4_TERMINAL_PINS.independent_snapshot) && terminal.discovery_screenshot_bytes === 38695 &&
    Object.entries(HISTORY_V5_PREDECESSORS).every(([key, value]) => same(terminal[key], value)));
  need(same(terminal.primary_process, { pid: PRIMARY.pid, start_ticks: PRIMARY.start_ticks, boot_id: BOOT }) &&
    terminal.primary_invocation_id === PRIMARY.invocation &&
    terminal.primary_fact_sha256 === '0882d96f8b61c5586ce514a4c320a9bc933c2610cf55f24bfbec80237e77da3a' &&
    terminal.media_fact_sha256 === '0f21473c43a050ad54f8985ee57e98addc6420e0cf33d6ee115db8cf8c0eff7d');
  const serviceKeys = ['ActiveState', 'ControlGroup', 'DropInPaths', 'ExecMainCode', 'ExecMainStatus', 'FragmentPath', 'Group', 'Id',
    'InvocationID', 'LoadState', 'MainPID', 'Restart', 'Result', 'SubState', 'Transient', 'User', 'WorkingDirectory'];
  const candidate = terminal.candidate, live = candidate?.properties;
  need(exact(candidate, ['binary_sha256', 'invocation_id', 'process', 'properties']) &&
    candidate.binary_sha256 === input.candidate.binary_sha256 && candidate.invocation_id === input.candidate.invocation_id &&
    same(candidate.process, input.candidate.process) && exact(live, serviceKeys) &&
    same(live, { ActiveState: 'active', ControlGroup: '/system.slice/goby-client-m3e.service', DropInPaths: '', ExecMainCode: '0', ExecMainStatus: '0',
      FragmentPath: '/etc/systemd/system/goby-client-m3e.service', Group: 'goby', Id: 'goby-client-m3e.service',
      InvocationID: input.candidate.invocation_id, LoadState: 'loaded', MainPID: String(input.candidate.process.pid), Restart: 'no',
      Result: 'success', SubState: 'running', Transient: 'no', User: 'goby', WorkingDirectory: '/var/lib/goby-test/client-m3e' }));
  need(exact(terminal.failed_units, [scope.controllerUnit, scope.workerUnit]));
  for (const [unit, original, invocation, directory] of [
    [scope.controllerUnit, priorInput.controller, scope.controllerInvocation, scope.tool],
    [scope.workerUnit, browser.node_process, scope.workerInvocation, scope.root],
  ]) {
    const failure = terminal.failed_units[unit], expected = { pid: original.pid, start_ticks: Number(original.start_ticks), boot_id: BOOT };
    need(exact(failure, ['old_process', 'old_process_gone', 'properties', 'recursive_cgroup']) && same(failure.old_process, expected) &&
      failure.old_process_gone === true && exact(failure.properties, serviceKeys) && same(failure.properties, {
        ActiveState: 'failed', ControlGroup: '', DropInPaths: '', ExecMainCode: '1', ExecMainStatus: '1',
        FragmentPath: '/run/systemd/transient/' + unit, Group: 'root', Id: unit, InvocationID: invocation, LoadState: 'loaded', MainPID: '0',
        Restart: 'no', Result: 'exit-code', SubState: 'failed', Transient: 'yes', User: 'root', WorkingDirectory: directory }) &&
      same(failure.recursive_cgroup, { exists: false, files_checked: 0, path: '/sys/fs/cgroup/system.slice/' + unit, processes: 0 }));
  }
  need(same(report.worker_terminal, { ...terminal.failed_units[scope.workerUnit].properties, cgroup_empty: true }));
  const observed = terminal.browser;
  need(exact(observed, ['browser_closed', 'capabilities_verified', 'capability_requests', 'context_closed', 'failure', 'failure_counters',
    'http_pending', 'login_proven', 'owned_session_revoked', 'proxy_closed', 'result', 'sockets_remaining',
    'ui_logout_and_token_rejection_proven', 'websocket_active', 'websocket_closed', 'websocket_opened', 'websocket_pending']) &&
    observed.result === 'failed' && observed.failure === 'library_changed_target_card_not_observed' && observed.capability_requests === 1 &&
    ['browser_closed', 'capabilities_verified', 'context_closed', 'login_proven', 'owned_session_revoked', 'proxy_closed',
      'ui_logout_and_token_rejection_proven'].every(key => observed[key] === true) &&
    ['http_pending', 'sockets_remaining', 'websocket_active', 'websocket_pending'].every(key => observed[key] === 0) &&
    observed.websocket_opened === 1 && observed.websocket_closed === 1 &&
    same(observed.failure_counters, { observer_errors: 0, page_errors: 0, proxy_failed: 0, proxy_rejected: 0, websocket_failed: 0 }));
}

function validateHistorySixTerminal(input, terminal, priorInput, browser, report, ledger, scope) {
  need(scope.version === 6 && exact(terminal, HISTORY_V6_TERMINAL_KEYS) &&
    Object.entries(HISTORY_V6_TERMINAL_SCALARS).every(([key, value]) => same(terminal[key], value)) &&
    Object.entries(HISTORY_V6_TERMINAL_FACTS).every(([key, value]) => same(terminal[key], value)) &&
    Object.entries(HISTORY_V6_PREDECESSORS).every(([key, value]) => same(terminal[key], value)) &&
    same(terminal.accepted_browser_stages, ['discovery']) && same(terminal.reserved_native_intents, ['login', 'logout', 'exact401']) &&
    same(terminal.dispatched_native_intents, ['login', 'logout', 'exact401']) && same(terminal.ledger, ledger) &&
    same(terminal.cumulative_totals, scope.afterCounts) && same(terminal.history_preservation, report.history_preservation) &&
    same(terminal.prior_baseline, HISTORY_V5_TERMINAL_PINS.independent_snapshot));
  ledgerInstant(terminal.captured_at);
  for (const [key, expected] of Object.entries({ input: scope.pins.prior_input, report: scope.pins.prior_controller_report,
    browser_report: scope.pins.prior_browser_report, before_snapshot: scope.pins.prior_before_snapshot,
    prior_after_snapshot: scope.pins.prior_after_snapshot, upgrade_authority_snapshot: UPGRADE_PINS.current_snapshot, ...scope.terminalPins }))
    need(same(terminal[key], expected));
  const native = terminal.native_authentication;
  need(same(native.private, report.evidence['native-session-private.json']) &&
    same(native.received_header, report.evidence['native-cookie-received-private.json']));
  for (const name of ['login', 'logout', 'exact401']) for (const kind of ['intent', 'result'])
    need(same(native.requests[name][kind], report.evidence['native-' + name + '-' + kind + '.json']));
  need(same(terminal.primary_process, { pid: PRIMARY.pid, start_ticks: PRIMARY.start_ticks, boot_id: BOOT }) &&
    terminal.primary_invocation_id === PRIMARY.invocation &&
    terminal.primary_fact_sha256 === '0882d96f8b61c5586ce514a4c320a9bc933c2610cf55f24bfbec80237e77da3a' &&
    terminal.media_fact_sha256 === '0f21473c43a050ad54f8985ee57e98addc6420e0cf33d6ee115db8cf8c0eff7d');
  const serviceKeys = ['ActiveState', 'ControlGroup', 'DropInPaths', 'ExecMainCode', 'ExecMainStatus', 'FragmentPath', 'Group', 'Id',
    'InvocationID', 'LoadState', 'MainPID', 'Restart', 'Result', 'SubState', 'Transient', 'User', 'WorkingDirectory'];
  const candidate = terminal.candidate, live = candidate?.properties;
  need(exact(candidate, ['binary_sha256', 'invocation_id', 'process', 'properties']) &&
    candidate.binary_sha256 === input.candidate.binary_sha256 && candidate.invocation_id === input.candidate.invocation_id &&
    same(candidate.process, input.candidate.process) && exact(live, serviceKeys) &&
    same(live, { ActiveState: 'active', ControlGroup: '/system.slice/goby-client-m3e.service', DropInPaths: '', ExecMainCode: '0', ExecMainStatus: '0',
      FragmentPath: '/etc/systemd/system/goby-client-m3e.service', Group: 'goby', Id: 'goby-client-m3e.service',
      InvocationID: input.candidate.invocation_id, LoadState: 'loaded', MainPID: String(input.candidate.process.pid), Restart: 'no',
      Result: 'success', SubState: 'running', Transient: 'no', User: 'goby', WorkingDirectory: '/var/lib/goby-test/client-m3e' }));
  need(exact(terminal.failed_units, [scope.controllerUnit, scope.workerUnit]));
  for (const [unit, original, invocation, directory] of [
    [scope.controllerUnit, priorInput.controller, scope.controllerInvocation, scope.tool],
    [scope.workerUnit, browser.node_process, scope.workerInvocation, scope.root],
  ]) {
    const failure = terminal.failed_units[unit], expected = { pid: original.pid, start_ticks: Number(original.start_ticks), boot_id: BOOT };
    need(exact(failure, ['old_process', 'old_process_gone', 'properties', 'recursive_cgroup']) && same(failure.old_process, expected) &&
      failure.old_process_gone === true && exact(failure.properties, serviceKeys) && same(failure.properties, {
        ActiveState: 'failed', ControlGroup: '', DropInPaths: '', ExecMainCode: '1', ExecMainStatus: '1',
        FragmentPath: '/run/systemd/transient/' + unit, Group: 'root', Id: unit, InvocationID: invocation, LoadState: 'loaded', MainPID: '0',
        Restart: 'no', Result: 'exit-code', SubState: 'failed', Transient: 'yes', User: 'root', WorkingDirectory: directory }) &&
      same(failure.recursive_cgroup, { exists: false, files_checked: 0, path: '/sys/fs/cgroup/system.slice/' + unit, processes: 0 }));
  }
  need(same(report.worker_terminal, { ...terminal.failed_units[scope.workerUnit].properties, cgroup_empty: true }));
  const observed = terminal.browser;
  need(exact(observed, ['browser_closed', 'capabilities_verified', 'capability_requests', 'context_closed', 'failure', 'failure_counters',
    'http_pending', 'login_proven', 'owned_session_revoked', 'proxy_closed', 'result', 'sockets_remaining',
    'ui_logout_and_token_rejection_proven', 'websocket_active', 'websocket_closed', 'websocket_opened', 'websocket_pending']) &&
    observed.result === 'failed' && observed.failure === 'library_changed_controller_aborted' && observed.capability_requests === 1 &&
    ['browser_closed', 'capabilities_verified', 'context_closed', 'login_proven', 'owned_session_revoked', 'proxy_closed',
      'ui_logout_and_token_rejection_proven'].every(key => observed[key] === true) &&
    ['http_pending', 'sockets_remaining', 'websocket_active', 'websocket_pending'].every(key => observed[key] === 0) &&
    observed.websocket_opened === 1 && observed.websocket_closed === 1 &&
    same(observed.failure_counters, { observer_errors: 0, page_errors: 0, proxy_failed: 0, proxy_rejected: 0, websocket_failed: 0 }));
}

function validateHistoryRecord(input, documents, scope) {
  if (scope.version === 6) return validateHistorySixRecord(input, documents, scope);
  const { input: priorInput, browser_report: browser, controller_report: report, terminal: priorTerminal, before: priorBefore, after: priorAfter, independent: priorIndependent } = documents;
  need(exact(priorInput, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture', 'expected_libraries', 'target',
    'source_closure', 'authority', 'controller']) && priorInput.marker === input.marker && priorInput.version === 1 && priorInput.mode === input.mode &&
    priorInput.root === scope.root && priorInput.output === scope.root + '/browser' && same(priorInput.candidate, input.candidate) &&
    same(priorInput.fixture, FIXTURES) && same(priorInput.expected_libraries, input.expected_libraries) && same(priorInput.target, input.target) &&
    same(priorInput.authority, historicalInputAuthority(scope.version)) &&
    exact(priorInput.source_closure, SOURCES.map(name => scope.tool + '/' + name)) && Object.values(priorInput.source_closure).every(digest));
  const actor = priorInput.actor, controller = priorInput.controller;
  need(exact(actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) && actor.slot === 'B' && actor.user_id === USER &&
    actor.account_key === 'viewer' && actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(actor.credentials) &&
    actor.credentials.path === scope.root + '/viewer-credentials.json' &&
    exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    typeof controller.start_ticks === 'string' && /^[1-9]\d*$/.test(controller.start_ticks) && Number.isSafeInteger(Number(controller.start_ticks)) &&
    controller.boot_id === BOOT && controller.unit === scope.controllerUnit);
  const closureSHA = hash(canonical(priorInput.source_closure)), node = browser?.node_process;
  need(record(browser) && browser.marker === 'goby-client-library-changed-report-v1' && browser.version === 1 && browser.mode === input.mode &&
    browser.result === 'failed' && browser.outcome === 'failed' && browser.failure === 'library_changed_target_card_not_observed' &&
    browser.input_sha256 === scope.pins.prior_input.sha256 && browser.source_closure_sha256 === closureSHA &&
    same(browser.controller, controller) && same(browser.candidate, input.candidate) && same(browser.authority, priorInput.authority) &&
    same(browser.target, input.target) && ['client_acceptance', 'library_changed_client_acceptance', 'full_m3_complete'].every(key => browser[key] === false) &&
    ['discovery', 'armed', 'forward', 'restore_armed', 'restored'].every(key => browser[key] === null) &&
    same(browser.stages, []) && same(browser.controls, []) && browser.restoration === 'not_required' &&
    exact(node, ['pid', 'start_ticks', 'boot_id', 'uid', 'gid', 'executable_path', 'executable_sha256', 'cgroup']) &&
    Number.isSafeInteger(node.pid) && node.pid > 1 && typeof node.start_ticks === 'string' && /^[1-9]\d*$/.test(node.start_ticks) &&
    Number.isSafeInteger(Number(node.start_ticks)) && node.boot_id === BOOT && node.uid === 0 && node.gid === 0 &&
    node.executable_path === WORK + '/client-library-changed-source44-tool-01/node' &&
    node.executable_sha256 === '3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327' && node.cgroup === '/system.slice/' + scope.workerUnit);
  validatePriorLogin(browser.login_proof);
  const observedActor = browser.actor, proof = browser.login_proof, token = proof.token_sha256;
  need(record(observedActor) && observedActor.slot === 'B' && observedActor.id === USER && observedActor.credentials_sha256 === actor.credentials.sha256 &&
    observedActor.ordinary_authority_confirmed === true && observedActor.closed === true && observedActor.token_fingerprint === token &&
    same(observedActor.cleanup_failures, []) && same(observedActor.login, { status: 200, request_count: 1, attempted: true, credentials_filled: true }) &&
    same(observedActor.logout, { status: 204, login_view_visible: true, attempted: true }) &&
    same(observedActor.proxy_logout, { source: 'physical-http-forwarding-proxy-request', token_fingerprint: token, status: 204, completed: true }));
  validatePriorClosure(browser.closure); validatePriorClosure(observedActor.home_closure);
  const logoutProof = observedActor.session_proof, entry = logoutProof?.entries?.[0], verification = entry?.verification, ui = entry?.ui_request;
  need(record(logoutProof) && logoutProof.format === 1 && logoutProof.outcome === 'all_observed_logout_tokens_rejected' &&
    Array.isArray(logoutProof.entries) && logoutProof.entries.length === 1 && logoutProof.logout_overflow === 0 && logoutProof.observer_errors === 0 &&
    entry.index === 0 && entry.token_fingerprint === token && entry.result === 'logout_token_rejected' &&
    same(entry.token_sources, ['query:x-emby-token']) && ui?.method === 'POST' && ui.route === '/emby/Sessions/Logout' && ui.response_status === 204 &&
    ui.client_request_finished === true && ui.client_request_failed === false &&
    verification?.source === 'independent-node-http-post-logout-verification' && verification.is_ui_request === false &&
    verification.method === 'GET' && verification.route === '/emby/System/Info' && verification.status === 401 &&
    verification.eligible_at_request_start === true && verification.result === 'token_rejected' && verification.trigger === 'ui_logout_response');
  const times = [ui.elapsed_ms, ui.response_elapsed_ms, verification.started_elapsed_ms, verification.finished_elapsed_ms];
  need(times.every(value => Number.isSafeInteger(value) && value >= 0 && value <= 600000) &&
    times.every((value, index) => index === 0 || value >= times[index - 1]));
  const capabilities = browser.capabilities_private;
  need(exact(capabilities, ['path', 'sha256', 'request_count', 'last_successful_body_sha256']) &&
    capabilities.path === scope.root + '/browser/capabilities-private.json' && digest(capabilities.sha256) &&
    capabilities.request_count === 1 && digest(capabilities.last_successful_body_sha256));
  if (scope.version === 3) need(exact(browser.diagnostics, ['discovery_failure', 'screenshot', 'status', 'reason']) &&
    same(browser.diagnostics.discovery_failure, { ...HISTORY_V3_PREDECESSORS.discovery_failure, bytes: 9503 }) &&
    browser.diagnostics.screenshot === null && browser.diagnostics.status === 'saved' && browser.diagnostics.reason === null);
  if (scope.version === 4) need(exact(browser.diagnostics, ['discovery_failure', 'screenshot', 'status', 'reason']) &&
    same(browser.diagnostics.discovery_failure, { ...HISTORY_V4_DIAGNOSTICS.discovery_failure, bytes: 19957 }) &&
    same(browser.diagnostics.screenshot, { ...HISTORY_V4_DIAGNOSTICS.screenshot, bytes: 38695 }) &&
    browser.diagnostics.status === 'saved' && browser.diagnostics.reason === null);
  if (scope.version === 5) need(exact(browser.diagnostics, ['discovery_failure', 'screenshot', 'status', 'reason']) &&
    same(browser.diagnostics.discovery_failure, { ...HISTORY_V5_DIAGNOSTICS.discovery_failure, bytes: 12134 }) &&
    same(browser.diagnostics.screenshot, { ...HISTORY_V5_DIAGNOSTICS.screenshot, bytes: 38695 }) &&
    browser.diagnostics.status === 'saved' && browser.diagnostics.reason === null);
  need(record(report) && report.marker === 'goby-client-library-changed-observation-v1' && report.version === 1 && report.mode === input.mode &&
    report.status === 'failed' && report.phase === 'discovery' && report.input_sha256 === scope.pins.prior_input.sha256 &&
    report.source_closure_sha256 === closureSHA && same(report.controller, controller) && same(report.node_process, node) &&
    same(report.candidate_process, input.candidate.process) && report.candidate_invocation === input.candidate.invocation_id &&
    report.state_sha256 === input.candidate.state_sha256 && same(report.authority, historicalControllerAuthority(scope.version)) &&
    same(report.reserved_native_intents, []) && same(report.dispatched_native_intents, []) && report.restoration === 'not_required' &&
    ['automatic_retry', 'browser_fallback_used', 'candidate_or_primary_service_writes', 'client_acceptance', 'full_m3_complete',
      'library_changed_client_acceptance', 'restoration_required', 'sql_business_writes', 'worker_chain_ledger_passed',
      'acceptance_ready_for_outer_terminal'].every(key => report[key] === false) && report.outer_controller_terminal_required === true && record(report.evidence));
  for (const [name, expected] of Object.entries({ 'input.json': scope.pins.prior_input, 'browser-report.json': scope.pins.prior_browser_report,
    'before-full.json': scope.pins.prior_before_snapshot, 'after-full.json': scope.pins.prior_after_snapshot,
    'browser-capabilities-private.json': { path: capabilities.path, sha256: capabilities.sha256 } })) need(same(report.evidence[name], expected));
  const close = browser.control_close, value = close?.value;
  need(exact(close, ['path', 'sha256', 'value']) && close.path === scope.root + '/control-close.json' && digest(close.sha256) &&
    same(report.evidence['control-close.json'], { path: close.path, sha256: close.sha256 }) &&
    exact(value, ['marker', 'version', 'name', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process',
      'previous_stage_sha256', 'reservation', 'commit', 'restoration']) && value.marker === 'goby-client-library-changed-control-v1' &&
    value.version === 1 && value.name === 'close' && value.input_sha256 === scope.pins.prior_input.sha256 && value.source_closure_sha256 === closureSHA &&
    same(value.controller, controller) && same(value.node_process, node) && value.previous_stage_sha256 === null && value.reservation === null &&
    value.commit === null && value.restoration === 'not_required');
  const ledger = validatePriorSnapshotDelta(priorBefore, priorAfter, browser, scope.version);
  need(same(report.ledger, ledger));
  if (scope.version === 3) need(same(report.prior_failure_preservation, ledger));
  if (scope.version === 4 || scope.version === 5) need(same(report.history_preservation, (scope.version === 4 ? [2, 3] : [2, 3, 4]).map(version => {
    const previous = historyScope(version);
    return { version, ledger, after_snapshot: previous.pins.prior_after_snapshot, independent_snapshot: previous.terminalPins.independent_snapshot };
  })));
  validatePriorTerminal(input, priorTerminal, priorInput, browser, report, ledger, scope);
  identicalSnapshotWithLaterCapture(priorAfter, priorIndependent);
  need(ledgerInstant(priorIndependent.database.metadata.captured_at) <= ledgerInstant(priorTerminal.captured_at));
  return ledger;
}

function validateHistorySixNativeIdentity(value, terminal, input, priorInput) {
  need(exact(value, [...Object.keys(HISTORY_V6_NATIVE_FACTS), 'candidate_process', 'controller']) &&
    Object.entries(HISTORY_V6_NATIVE_FACTS).every(([key, expected]) => same(value[key], expected)) &&
    same(value.candidate_process, input.candidate.process) && same(value.controller, priorInput.controller) &&
    same(value.native_private, terminal.native_authentication.private) && same(value.received_header, terminal.native_authentication.received_header));
  for (const name of ['login', 'logout', 'exact401']) {
    const { body_sha256: ignoredHash, body_bytes: ignoredBytes, ...transport } = value.requests[name];
    need(same(transport, terminal.native_authentication.requests[name]));
  }
  return HISTORY_V6_NATIVE_IDENTITY;
}

function validateHistorySixDiscovery(discovery, browser, report, priorInput) {
  const pins = HISTORY_V6_DISCOVERY, value = discovery?.value, observed = browser.discovery, token = browser.login_proof.token_sha256;
  need(exact(discovery, ['path', 'sha256', 'value']) && same({ path: discovery.path, sha256: discovery.sha256 }, pins.stage) &&
    same(report.evidence['accepted-stage-discovery.json'], pins.accepted) &&
    exact(value, ['controller', 'input_sha256', 'marker', 'name', 'node_process', 'observation', 'previous_control_sha256',
      'session_private', 'source_closure_sha256', 'token_sha256', 'version']) &&
    value.marker === 'goby-client-library-changed-stage-v1' && value.version === 1 && value.name === 'discovery' &&
    value.input_sha256 === HISTORY_V6_PINS.prior_input.sha256 && value.source_closure_sha256 === browser.source_closure_sha256 &&
    same(value.controller, priorInput.controller) && same(value.node_process, browser.node_process) && value.previous_control_sha256 === null &&
    value.token_sha256 === token && same(value.session_private, pins.session_private) && same(browser.session_private, pins.session_private) &&
    same(report.evidence['browser-session-private.json'], pins.session_private) && same(value.observation, observed) &&
    exact(observed, ['collection_folder', 'collection_folder_reads', 'dom', 'home', 'navigation', 'query_allowlist', 'reads', 'socket']));
  need(Array.isArray(browser.stage_publication_attempts) && browser.stage_publication_attempts.length === 1);
  const published = browser.stage_publication_attempts[0];
  need(exact(published, ['name', 'path', 'sha256', 'publication_started_elapsed_ms', 'completed']) &&
    published.name === 'discovery' && published.path === pins.stage.path && published.sha256 === pins.stage.sha256 && published.completed === true &&
    Number.isFinite(published.publication_started_elapsed_ms) && published.publication_started_elapsed_ms >= 0);
  need(same(browser.abort, pins.browser_abort) && same(report.evidence['browser-abort.json'], pins.browser_abort) &&
    same(report.evidence['abort.json'], pins.abort) && exact(browser.controller_abort, ['path', 'sha256', 'value']) &&
    same({ path: browser.controller_abort.path, sha256: browser.controller_abort.sha256 }, pins.abort));
  const aborted = browser.controller_abort.value;
  need(exact(aborted, ['controller', 'failure', 'input_sha256', 'marker', 'name', 'node_process', 'previous_control_sha256',
    'previous_stage_sha256', 'session_private', 'source_closure_sha256', 'token_sha256', 'version']) &&
    aborted.marker === 'goby-client-library-changed-abort-v1' && aborted.version === 1 && aborted.name === 'discovery' &&
    aborted.failure === 'library_changed_controller_failed' && aborted.input_sha256 === value.input_sha256 &&
    aborted.source_closure_sha256 === value.source_closure_sha256 && aborted.token_sha256 === token &&
    same(aborted.controller, value.controller) && same(aborted.node_process, value.node_process) &&
    same(aborted.session_private, value.session_private) && aborted.previous_control_sha256 === null && aborted.previous_stage_sha256 === pins.stage.sha256);
  const dom = observed.dom, home = observed.home, target = priorInput.target;
  const route = '/web/index.html#!/videos?serverId=' + SERVER + '&parentId=' + LIBRARY;
  need(record(dom) && dom.route === route && text(dom.document_id) && dom.target_id === ITEM && dom.expected_name === target.name &&
    dom.observed_title === target.name && dom.identity_mode === 'singleton-movie-list-wire-and-card' &&
    Number.isSafeInteger(dom.visible_items_containers) && dom.visible_items_containers >= 0 && dom.visible_items_containers <= 32 &&
    ['visible_card_containers', 'visible_cards', 'visible_title_buttons', 'visible_target_cards', 'target_title_count'].every(key => dom[key] === 1) &&
    dom.forbidden_title_count === 0 && ['explicit_identity_consistent', 'identity_proven', 'media_inactive', 'passed'].every(key => dom[key] === true) &&
    record(home) && home.passed === true && home.media_inactive === true && home.location?.route === 'home' &&
    home.location.same_origin === true && home.location.supported_path === true && Array.isArray(home.libraries) && home.libraries.length === 4 &&
    priorInput.expected_libraries.every(expected => home.libraries.filter(library => library.id === expected.id && library.name === expected.name &&
      library.visible_card_count === 1 && library.card_id_present === true && library.card_id_matches === true && library.passed === true).length === 1));
  const pairs = [observed.reads, observed.collection_folder_reads];
  need(pairs.every(items => Array.isArray(items) && items.length === 1));
  for (const [index, items] of pairs.entries()) {
    const pair = items[0], frame = pair?.frame, physical = pair?.physical, kind = index === 0 ? 'items' : 'collection-folder';
    const requestRoute = '/Users/' + USER + '/Items' + (index === 0 ? '' : '/' + LIBRARY);
    need(exact(pair, ['frame', 'physical', 'unambiguous', 'complete']) && pair.unambiguous === true && pair.complete === true &&
      record(frame) && record(physical) && Number.isSafeInteger(frame.index) && frame.index >= 0 && Number.isSafeInteger(physical.id) && physical.id >= 0 &&
      [frame, physical].every(part => part.kind === kind && part.route === requestRoute && part.phase === 'discovery' && part.token_sha256 === token &&
        part.status === 200 && digest(part.shape_sha256) && digest(part.request_sha256)) &&
      frame.shape_sha256 === physical.shape_sha256 && frame.request_sha256 === physical.request_sha256 && frame.main_frame === true &&
      frame.document_id === dom.document_id && frame.page_route === route && frame.finished === true && frame.failed === false && frame.from_service_worker === false &&
      physical.method === 'GET' && physical.completed === true && physical.terminal === 'completed' && physical.terminal_status === 200 &&
      record(physical.projection) && physical.projection.count === 1 && digest(physical.projection.body_sha256) &&
      Number.isSafeInteger(physical.projection.body_bytes) && physical.projection.body_bytes > 0 &&
      physical.response_bytes === physical.projection.body_bytes && Array.isArray(physical.query));
    if (index === 0) {
      const query = new Map(physical.query);
      need(query.size === physical.query.length && !query.has('Ids') && query.get('ParentId') === LIBRARY && query.get('IncludeItemTypes') === 'Movie' &&
        query.get('Recursive') === 'true' && query.get('StartIndex') === '0' && query.get('Limit') === '50' &&
        same(physical.projection.target, { Id: ITEM, Name: target.name, Type: 'Movie' }) &&
        same(dom.wire_identity, { phase: 'discovery', physical_exchange_id: physical.id, frame_request_index: frame.index,
          body_sha256: physical.projection.body_sha256, shape_sha256: physical.shape_sha256, request_sha256: physical.request_sha256,
          token_sha256: token, message_id: null }) &&
        same(observed.query_allowlist, [{ kind, route: requestRoute, query: physical.query, shape_sha256: physical.shape_sha256 }]));
    } else {
      const collection = physical.projection.collection_folder;
      need(same(physical.query, []) && collection?.Id === LIBRARY && collection.Type === 'CollectionFolder' &&
        collection.Name === priorInput.expected_libraries.find(library => library.id === LIBRARY).name &&
        same(collection.Subviews, ['movies', 'movies', 'folders']) &&
        same(observed.collection_folder, { id: LIBRARY, type: 'CollectionFolder', subviews: collection.Subviews,
          physical_exchange_id: physical.id, frame_request_index: frame.index, passed: true }));
    }
  }
  need(exact(observed.socket, ['connection_id', 'token_sha256', 'seen', 'opened']) && text(observed.socket.connection_id) &&
    observed.socket.token_sha256 === token && observed.socket.seen === 1 && observed.socket.opened === 1 &&
    observed.navigation?.before_route === '/web/index.html#!/home' && observed.navigation.after_route === route);
}

function validateHistorySixRecord(input, documents, scope) {
  need(scope.version === 6);
  const { input: priorInput, browser_report: browser, controller_report: report, terminal: priorTerminal, before: priorBefore, after: priorAfter, independent: priorIndependent } = documents;
  need(exact(priorInput, ['marker', 'version', 'mode', 'root', 'output', 'actor', 'candidate', 'fixture', 'expected_libraries', 'target',
    'source_closure', 'authority', 'controller']) && priorInput.marker === input.marker && priorInput.version === 1 && priorInput.mode === input.mode &&
    priorInput.root === scope.root && priorInput.output === scope.root + '/browser' && same(priorInput.candidate, input.candidate) &&
    same(priorInput.fixture, FIXTURES) && same(priorInput.expected_libraries, input.expected_libraries) && same(priorInput.target, input.target) &&
    same(priorInput.authority, historicalInputAuthority(scope.version)) &&
    exact(priorInput.source_closure, SOURCES.map(name => scope.tool + '/' + name)) && Object.values(priorInput.source_closure).every(digest));
  const actor = priorInput.actor, controller = priorInput.controller;
  need(exact(actor, ['slot', 'user_id', 'credentials', 'account_key', 'source_credentials_sha256']) && actor.slot === 'B' && actor.user_id === USER &&
    actor.account_key === 'viewer' && actor.source_credentials_sha256 === SOURCE_CREDENTIALS_SHA && descriptor(actor.credentials) &&
    actor.credentials.path === scope.root + '/viewer-credentials.json' &&
    exact(controller, ['pid', 'start_ticks', 'boot_id', 'unit']) && Number.isSafeInteger(controller.pid) && controller.pid > 1 &&
    typeof controller.start_ticks === 'string' && /^[1-9]\d*$/.test(controller.start_ticks) && Number.isSafeInteger(Number(controller.start_ticks)) &&
    controller.boot_id === BOOT && controller.unit === scope.controllerUnit);
  const closureSHA = hash(canonical(priorInput.source_closure)), node = browser?.node_process;
  need(record(browser) && browser.marker === 'goby-client-library-changed-report-v1' && browser.version === 1 && browser.mode === input.mode &&
    browser.result === 'failed' && browser.outcome === 'failed' && browser.failure === 'library_changed_controller_aborted' &&
    browser.input_sha256 === scope.pins.prior_input.sha256 && browser.source_closure_sha256 === closureSHA &&
    same(browser.controller, controller) && same(browser.candidate, input.candidate) && same(browser.authority, priorInput.authority) &&
    same(browser.target, input.target) && ['client_acceptance', 'library_changed_client_acceptance', 'full_m3_complete'].every(key => browser[key] === false) &&
    ['armed', 'forward', 'restore_armed', 'restored'].every(key => browser[key] === null) &&
    same(browser.stages, [{ name: 'discovery', ...HISTORY_V6_DISCOVERY.stage }]) && same(browser.controls, []) && browser.restoration === 'not_required' &&
    exact(node, ['pid', 'start_ticks', 'boot_id', 'uid', 'gid', 'executable_path', 'executable_sha256', 'cgroup']) &&
    Number.isSafeInteger(node.pid) && node.pid > 1 && typeof node.start_ticks === 'string' && /^[1-9]\d*$/.test(node.start_ticks) &&
    Number.isSafeInteger(Number(node.start_ticks)) && node.boot_id === BOOT && node.uid === 0 && node.gid === 0 &&
    node.executable_path === WORK + '/client-library-changed-source44-tool-01/node' &&
    node.executable_sha256 === '3517c2df0b2f8cd7f422b4b8450ef81c6889f08eb03e281d6de9079b15e6a327' && node.cgroup === '/system.slice/' + scope.workerUnit);
  validatePriorLogin(browser.login_proof);
  const observedActor = browser.actor, proof = browser.login_proof, token = proof.token_sha256;
  need(record(observedActor) && observedActor.slot === 'B' && observedActor.id === USER && observedActor.credentials_sha256 === actor.credentials.sha256 &&
    observedActor.ordinary_authority_confirmed === true && observedActor.closed === true && observedActor.token_fingerprint === token &&
    same(observedActor.cleanup_failures, []) && same(observedActor.login, { status: 200, request_count: 1, attempted: true, credentials_filled: true }) &&
    same(observedActor.logout, { status: 204, login_view_visible: true, attempted: true }) &&
    same(observedActor.proxy_logout, { source: 'physical-http-forwarding-proxy-request', token_fingerprint: token, status: 204, completed: true }));
  validatePriorClosure(browser.closure); validatePriorClosure(observedActor.home_closure);
  const logoutProof = observedActor.session_proof, entry = logoutProof?.entries?.[0], verification = entry?.verification, ui = entry?.ui_request;
  need(record(logoutProof) && logoutProof.format === 1 && logoutProof.outcome === 'all_observed_logout_tokens_rejected' &&
    Array.isArray(logoutProof.entries) && logoutProof.entries.length === 1 && logoutProof.logout_overflow === 0 && logoutProof.observer_errors === 0 &&
    entry.index === 0 && entry.token_fingerprint === token && entry.result === 'logout_token_rejected' &&
    same(entry.token_sources, ['query:x-emby-token']) && ui?.method === 'POST' && ui.route === '/emby/Sessions/Logout' && ui.response_status === 204 &&
    ui.client_request_finished === true && ui.client_request_failed === false &&
    verification?.source === 'independent-node-http-post-logout-verification' && verification.is_ui_request === false &&
    verification.method === 'GET' && verification.route === '/emby/System/Info' && verification.status === 401 &&
    verification.eligible_at_request_start === true && verification.result === 'token_rejected' && verification.trigger === 'ui_logout_response');
  const times = [ui.elapsed_ms, ui.response_elapsed_ms, verification.started_elapsed_ms, verification.finished_elapsed_ms];
  need(times.every(value => Number.isSafeInteger(value) && value >= 0 && value <= 600000) &&
    times.every((value, index) => index === 0 || value >= times[index - 1]));
  const capabilities = browser.capabilities_private;
  need(exact(capabilities, ['path', 'sha256', 'request_count', 'last_successful_body_sha256']) &&
    capabilities.path === scope.root + '/browser/capabilities-private.json' && digest(capabilities.sha256) &&
    capabilities.request_count === 1 && digest(capabilities.last_successful_body_sha256));
  need(!Object.hasOwn(browser, 'diagnostics'));
  need(record(report) && report.marker === 'goby-client-library-changed-observation-v1' && report.version === 1 && report.mode === input.mode &&
    report.status === 'failed' && report.phase === 'discovery' && report.input_sha256 === scope.pins.prior_input.sha256 &&
    report.source_closure_sha256 === closureSHA && same(report.controller, controller) && same(report.node_process, node) &&
    same(report.candidate_process, input.candidate.process) && report.candidate_invocation === input.candidate.invocation_id &&
    report.state_sha256 === input.candidate.state_sha256 && same(report.authority, historicalControllerAuthority(scope.version)) &&
    same(report.reserved_native_intents, ['login', 'logout', 'exact401']) && same(report.dispatched_native_intents, ['login', 'logout', 'exact401']) && report.restoration === 'not_required' &&
    ['automatic_retry', 'browser_fallback_used', 'candidate_or_primary_service_writes', 'client_acceptance', 'full_m3_complete',
      'library_changed_client_acceptance', 'restoration_required', 'sql_business_writes', 'worker_chain_ledger_passed',
      'acceptance_ready_for_outer_terminal'].every(key => report[key] === false) && report.outer_controller_terminal_required === true && record(report.evidence));
  for (const [name, expected] of Object.entries({ 'input.json': scope.pins.prior_input, 'browser-report.json': scope.pins.prior_browser_report,
    'before-full.json': scope.pins.prior_before_snapshot, 'after-full.json': scope.pins.prior_after_snapshot,
    'browser-capabilities-private.json': { path: capabilities.path, sha256: capabilities.sha256 } })) need(same(report.evidence[name], expected));
  const close = browser.control_close, value = close?.value;
  need(exact(close, ['path', 'sha256', 'value']) && close.path === scope.root + '/control-close.json' && digest(close.sha256) &&
    same(report.evidence['control-close.json'], { path: close.path, sha256: close.sha256 }) &&
    exact(value, ['marker', 'version', 'name', 'input_sha256', 'source_closure_sha256', 'controller', 'node_process',
      'previous_stage_sha256', 'reservation', 'commit', 'restoration']) && value.marker === 'goby-client-library-changed-control-v1' &&
    value.version === 1 && value.name === 'close' && value.input_sha256 === scope.pins.prior_input.sha256 && value.source_closure_sha256 === closureSHA &&
    same(value.controller, controller) && same(value.node_process, node) && value.previous_stage_sha256 === HISTORY_V6_DISCOVERY.stage.sha256 && value.reservation === null &&
    value.commit === null && value.restoration === 'not_required');
  validateHistorySixDiscovery(documents.discovery, browser, report, priorInput);
  const identity = validateHistorySixNativeIdentity(documents.native_identity, priorTerminal, input, priorInput);
  const ledger = validateHistorySixSnapshotDelta(priorBefore, priorAfter, browser, identity);
  need(same(report.ledger, ledger) && same(report.errors, [HISTORY_V6_TERMINAL_FACTS.failure]) &&
    same(report.history_preservation, [2, 3, 4, 5].map(version => {
      const previous = historyScope(version);
      return { version, ledger: { new_sessions: 1, new_devices: 1, new_audits: 2, metadata_revision_delta: 0,
        old_rows_sequences_private_preserved: true, owned_sessions_closed: true },
        after_snapshot: previous.pins.prior_after_snapshot, independent_snapshot: previous.terminalPins.independent_snapshot };
    })));
  validateHistorySixTerminal(input, priorTerminal, priorInput, browser, report, ledger, scope);
  identicalSnapshotWithLaterCapture(priorAfter, priorIndependent);
  need(ledgerInstant(priorIndependent.database.metadata.captured_at) <= ledgerInstant(priorTerminal.captured_at));
  return ledger;
}

function validatePriorDocuments(input, documents) {
  need(Array.isArray(documents.history) && documents.history.length === 5);
  let previous = documents.current, independent = null;
  const summaries = [];
  for (const [index, entry] of documents.history.entries()) {
    need(exact(entry, ['version', 'input', 'browser_report', 'controller_report', 'terminal', 'before', 'after', 'independent', ...(entry.version === 6 ? ['discovery', 'native_identity'] : [])]) && entry.version === index + 2);
    const scope = historyScope(entry.version);
    identicalSnapshotWithLaterCapture(previous, entry.before);
    if (independent !== null) need(ledgerInstant(entry.before.database.metadata.captured_at) > ledgerInstant(independent.database.metadata.captured_at));
    const ledger = validateHistoryRecord(input, entry, scope);
    catalogSequences(documents.catalog, entry.after.database);
    summaries.push({ version: entry.version, status: 'failed_scope_sealed', ledger });
    previous = entry.after; independent = entry.independent;
  }
  identicalSnapshotWithLaterCapture(previous, documents.before);
  need(ledgerInstant(documents.before.database.metadata.captured_at) > ledgerInstant(independent.database.metadata.captured_at));
  return summaries;
}

function validateUpgradeIntent(input, intent) {
  const scope = upgradeScope(input.authority), candidate = input.candidate;
  need(record(intent) && intent.marker === UPGRADE_MARKER && intent.version === 1 && intent.run_id === scope.run &&
    intent.tool === UPGRADE_TOOL && intent.output === scope.output && exact(intent.controller, ['unit']) && intent.controller.unit === scope.unit &&
    exact(intent.candidate, ['state', 'baseline', 'process', 'invocation_id']) &&
    same(intent.candidate.state, { path: STATE, sha256: PREVIOUS.state_sha256 }) &&
    same(intent.candidate.baseline, { path: WORK + '/collection-folder-contract-v1/after-full.json',
      sha256: '2659f45dfa82d8568b07375d04c08cd4ba216defcfa2a4a1c269867b176573be' }) &&
    same(intent.candidate.process, PREVIOUS.process) && intent.candidate.invocation_id === PREVIOUS_PROCESS.invocation);
  const source = intent.source;
  need(exact(source, ['root', 'manifest_sha256', 'full_report', 'terminal', 'binary', 'publication']) &&
    source.root === SOURCE && source.manifest_sha256 === SOURCE_SHA &&
    descriptor(source.full_report) && source.full_report.path === WORK + '/client-backup-run-20260912_084241_db776aacc1a7/report.json' &&
    descriptor(source.terminal) && source.terminal.path === WORK + '/collection-folder-source55-full-execution-01/terminal.json' &&
    descriptor(source.publication) && source.publication.path === UPGRADE_TOOL + '/publication.json' &&
    exact(source.binary, ['path', 'sha256', 'bytes']) && source.binary.sha256 === candidate.binary_sha256 &&
    source.binary.path === WORK + '/client-backup-run-20260912_084241_db776aacc1a7/tmp/goby-linux-amd64' &&
    Number.isSafeInteger(source.binary.bytes) && source.binary.bytes > 1024 * 1024 && source.binary.bytes <= 128 * 1024 * 1024);
  return scope;
}

function validateMovieQuerySingleton(tables, target) {
  need(record(tables) && Array.isArray(tables.items) && target?.id === ITEM && target.library_id === LIBRARY);
  const movies = tables.items.filter(row => row.library_id === LIBRARY && row.type === 'Movie');
  need(movies.length === 1 && movies[0].id === ITEM && movies[0].is_folder === false &&
    ['name', 'parent_id', 'root_id', 'relative_path'].every(key => movies[0][key] === target[key]) &&
    tables.items.filter(row => row.library_id === LIBRARY && row.name === target.name).length === 1);
  return true;
}

export function validateLibraryChangedSource55Documents(input, documents) {
  validateLibraryChangedSource55Input(input);
  need(exact(documents, ['state', 'beforeState', 'intent', 'report', 'attestation', 'publication', 'catalog', 'current', 'before',
    'profileReceipt', 'profileReport', 'profileInspection', 'musicChain', 'musicScanReceipt', 'proxy', 'history']));
  const { state, beforeState, intent, report, attestation, publication, catalog, current, before, profileReceipt, profileReport,
    profileInspection, musicChain, musicScanReceipt, proxy } = documents;
  const candidate = input.candidate, authority = input.authority, scope = validateUpgradeIntent(input, intent);
  need(record(state) && state.marker === 'goby-m3e-client-acceptance-v1' && state.work === WORK && state.schema === 28 &&
    state.phase === 'ready' && state.stage === 'complete' && state.server_id === SERVER && state.viewer_id === USER &&
    state.binary_sha256 === candidate.binary_sha256 && state.runtime_sha256 === candidate.runtime_sha256 &&
    state.browser_sha256 === SOURCE_CREDENTIALS_SHA && same(state.process, candidate.process) && record(state.work_identity));
  sourceBinding(state.schema28_source, candidate.publication);
  need(record(beforeState) && beforeState.schema === 27 && beforeState.phase === 'ready' && beforeState.stage === 'complete' &&
    beforeState.binary_sha256 === PREVIOUS.binary_sha256 && beforeState.runtime_sha256 === PREVIOUS.runtime_sha256 &&
    same(beforeState.process, PREVIOUS.process) && beforeState.schema27_source?.source === PREVIOUS.source &&
    beforeState.schema27_source.source_manifest_sha256 === PREVIOUS.source_manifest_sha256 &&
    beforeState.notifications_upgrade?.marker === 'goby-client-notifications-source44-continuation-v1');
  const allowed = new Set(['schema', 'phase', 'stage', 'process', 'binary_sha256', 'binary_identity', 'runtime_sha256', 'upgrade', 'upgrade_history']);
  need(same(Object.keys(state).sort(), [...Object.keys(beforeState), 'schema28_upgrade', 'schema28_source'].sort()) &&
    Object.entries(beforeState).every(([key, value]) => allowed.has(key) || same(state[key], value)));
  const upgrade = state.upgrade;
  need(exact(upgrade, ['marker', 'phase', 'from_schema', 'to_schema', 'output', 'intent_sha256', 'old_process', 'new_process',
    'from_sha256', 'to_sha256', 'publication', 'backup_sha256', 'rehearsal', 'service']) &&
    upgrade.marker === UPGRADE_MARKER && upgrade.phase === 'complete' && upgrade.from_schema === 27 && upgrade.to_schema === 28 &&
    upgrade.output === scope.output && upgrade.intent_sha256 === authority.upgrade_intent.sha256 &&
    same(upgrade.old_process, PREVIOUS.process) && same(upgrade.new_process, candidate.process) &&
    upgrade.from_sha256 === PREVIOUS.binary_sha256 && upgrade.to_sha256 === candidate.binary_sha256 &&
    upgrade.publication === candidate.publication && digest(upgrade.backup_sha256) &&
    descriptor(upgrade.rehearsal) && upgrade.rehearsal.path === scope.output + '/rehearsal-completed.json' &&
    same(state.schema28_upgrade, upgrade) && Array.isArray(beforeState.upgrade_history) &&
    same(state.upgrade_history, [...beforeState.upgrade_history, upgrade]));
  need(record(report) && report.marker === UPGRADE_MARKER && report.version === 1 && report.run_id === scope.run &&
    report.intent_sha256 === authority.upgrade_intent.sha256 && report.status === 'awaiting_outer_attestation' && report.schema === 28 &&
    report.state_sha256 === candidate.state_sha256 && same(report.binary, intent.source.binary) &&
    report.publication === candidate.publication && same(report.new_process, candidate.process) &&
    report.http_requests === 5 && same(report.service_actions, ['stop', 'start']) && report.candidate_web === '/opt/goby-client-m3e/admin' &&
    ['preserved_rows_sequences_credentials_recovery', 'primary_unchanged', 'shared_web_unchanged', 'rehearsal_removed',
      'hba_restored_exactly'].every(key => report[key] === true) &&
    ['client_acceptance', 'automatic_retry', 'automatic_rollback'].every(key => report[key] === false) &&
    record(report.evidence) && same(report.evidence['after-full.json'], authority.current_snapshot) &&
    same(report.evidence['before-state.json'], { path: scope.output + '/before-state.json', sha256: PREVIOUS.state_sha256 }) &&
    same(report.evidence['rehearsal-completed.json'], upgrade.rehearsal));
  need(Object.entries(report.evidence).every(([name, item]) => /^[a-z0-9][a-z0-9.-]{0,95}$/.test(name) && descriptor(item) &&
    [scope.output, scope.output + '/private'].includes(path.posix.dirname(item.path))));
  const service = report.candidate_service;
  need(exact(service, ['MainPID', 'InvocationID', 'ActiveState', 'SubState']) && service.MainPID === String(candidate.process.pid) &&
    service.InvocationID === candidate.invocation_id && service.ActiveState === 'active' && service.SubState === 'running' &&
    same(upgrade.service, service));
  const controller = report.controller, processFact = controller?.process;
  need(exact(controller, ['unit', 'invocation_id', 'process']) && controller.unit === scope.unit &&
    typeof controller.invocation_id === 'string' && ID.test(controller.invocation_id) &&
    !/^([0-9a-f])\1{31}$/.test(controller.invocation_id) && controller.invocation_id !== candidate.invocation_id &&
    exact(processFact, ['pid', 'start_ticks', 'boot_id', 'uid', 'exe', 'cgroup', 'namespace']) &&
    processIdentity({ pid: processFact.pid, start_ticks: processFact.start_ticks, boot_id: processFact.boot_id }) &&
    ![input.controller.pid, candidate.process.pid, PREVIOUS.process.pid, PRIMARY.pid, PROXY_PROCESS.pid].includes(processFact.pid) &&
    processFact.uid === 0 && text(processFact.exe, 4096) && processFact.exe.startsWith('/') &&
    path.posix.normalize(processFact.exe) === processFact.exe && !processFact.exe.includes('\\') &&
    processFact.cgroup === '/system.slice/' + scope.unit && /^mnt:\[\d+\]$/.test(processFact.namespace));
  need(exact(attestation, ['marker', 'version', 'run_id', 'intent_sha256', 'status', 'report_sha256', 'controller',
    'recursive_cgroup_empty', 'state_sha256', 'binary', 'schema', 'rows_sequences_credentials_recovery_preserved',
    'primary_unchanged', 'rehearsal_removed', 'hba_restored_exactly', 'client_acceptance']) &&
    attestation.marker === UPGRADE_MARKER && attestation.version === 1 && attestation.run_id === scope.run &&
    attestation.intent_sha256 === authority.upgrade_intent.sha256 && attestation.status === 'passed' &&
    attestation.report_sha256 === authority.upgrade_report.sha256 && attestation.state_sha256 === candidate.state_sha256 &&
    same(attestation.binary, intent.source.binary) && attestation.schema === 28 && attestation.client_acceptance === false &&
    ['recursive_cgroup_empty', 'rows_sequences_credentials_recovery_preserved', 'primary_unchanged', 'rehearsal_removed',
      'hba_restored_exactly'].every(key => attestation[key] === true));
  const terminal = attestation.controller;
  need(exact(terminal, ['MainPID', 'InvocationID', 'ActiveState', 'SubState', 'Result', 'ExecMainStatus', 'ControlGroup',
    'Description', 'RemainAfterExit']) && terminal.MainPID === '0' && terminal.InvocationID === controller.invocation_id &&
    terminal.ActiveState === 'active' && terminal.SubState === 'exited' && terminal.Result === 'success' && terminal.ExecMainStatus === '0' &&
    terminal.RemainAfterExit === 'yes' && terminal.Description === UPGRADE_MARKER + ':' + scope.run &&
    ['', '/system.slice/' + scope.unit].includes(terminal.ControlGroup));
  need(exact(publication, ['marker', 'commit', 'source_manifest_sha256', 'binary_sha256', 'full_report_sha256', 'full_terminal_sha256']) &&
    publication.marker === 'goby-source55-publication-v1' && publication.commit === candidate.publication &&
    publication.source_manifest_sha256 === SOURCE_SHA && publication.binary_sha256 === candidate.binary_sha256 &&
    publication.full_report_sha256 === intent.source.full_report.sha256 && publication.full_terminal_sha256 === intent.source.terminal.sha256);
  // These are historical receipt anchors, not current source32 runtime authority.
  need(profileReceipt?.marker === 'goby-client-special-features-fixture-v1' && profileReceipt.phase === 'complete' &&
    profileReceipt.schema === 27 && profileReceipt.profile_version === 3 &&
    state.special_features_profile?.receipt_path === FIXTURES.profile_receipt.path &&
    state.special_features_profile.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    profileReport?.result === 'passed' && profileReport.phase === 'complete' &&
    profileReport.receipt_sha256 === FIXTURES.profile_receipt.sha256 && profileInspection?.result === 'passed' &&
    profileInspection.phase === 'complete' && profileInspection.schema === 27 &&
    profileInspection.receipt_sha256 === FIXTURES.profile_receipt.sha256 &&
    Array.isArray(musicChain) && musicChain.length === 5 &&
    musicChain[4].completedSHA256 === profileReceipt.upgrade?.completed_sha256 &&
    musicChain[4].reportSHA256 === profileReceipt.upgrade?.report_sha256 &&
    musicScanReceipt?.schema === 25 && musicScanReceipt.phase === 'complete' && musicScanReceipt.result === 'passed' &&
    musicScanReceipt.logout_status === 204 && musicScanReceipt.token_readback_status === 401);
  need(proxy?.pid === PROXY_PROCESS.pid && String(proxy.start_ticks) === String(PROXY_PROCESS.start_ticks) &&
    proxy.reference_only === false && Array.isArray(proxy.listen) && proxy.listen.length <= 4 &&
    proxy.listen.filter(value => value === '127.0.0.1:18196').length === 1);
  need(Array.isArray(documents.history) && documents.history.length === 5);
  validateSchema28Snapshots(current, documents.history[0].before, catalog, candidate);
  validatePriorDocuments(input, documents);
  const tables = before.database.tables;
  validateMovieQuerySingleton(tables, input.target);
  const user = tables.users?.find(value => value.id === USER), target = tables.items.find(value => value.id === ITEM);
  need(user?.is_administrator === false && user.is_disabled === false && user.management_revision === 5 && target?.type === 'Movie' &&
    target.is_folder === false && target.library_id === LIBRARY && target.name === input.target.name &&
    target.parent_id === input.target.parent_id && target.root_id === input.target.root_id && target.relative_path === input.target.relative_path &&
    same(tables.libraries.map(value => ({ id: value.id, name: value.name })).sort((a, b) => a.id.localeCompare(b.id)),
      [...input.expected_libraries].sort((a, b) => a.id.localeCompare(b.id))));
  return true;
}
/** Reject duplicate decoded keys and retain exact large integral ledger values. */
export function parseSource55JSON(raw, allowArray = false) {
  need(typeof raw === 'string' && Buffer.byteLength(raw, 'utf8') <= SNAPSHOT_LIMIT);
  let position = 0, nodes = 0;
  const whitespace = () => { while (position < raw.length && /[ \t\r\n]/.test(raw[position])) position += 1; };
  function string() {
    const start = position++; let finished = false;
    while (position < raw.length) {
      if (raw[position] === '\\') { position += 2; continue; }
      if (raw[position++] === '"') { finished = true; break; }
    }
    need(finished);
    try { return JSON.parse(raw.slice(start, position)); } catch { need(false); }
  }
  function value(depth) {
    need(depth <= 64 && ++nodes <= 500000); whitespace();
    if (raw[position] === '"') return string();
    const opening = raw[position];
    if (opening === '{' || opening === '[') {
      position += 1; whitespace();
      const closing = opening === '{' ? '}' : ']', result = opening === '{' ? Object.create(null) : [];
      const keys = new Set();
      if (raw[position] === closing) { position += 1; return result; }
      for (;;) {
        let key;
        if (opening === '{') {
          need(raw[position] === '"'); key = string(); need(!keys.has(key)); keys.add(key);
          whitespace(); need(raw[position++] === ':');
        }
        const child = value(depth + 1);
        if (opening === '{') result[key] = child; else result.push(child);
        whitespace(); if (raw[position] === closing) { position += 1; return result; }
        need(raw[position++] === ','); whitespace();
      }
    }
    const token = /^(?:true|false|null|-?(?:0|[1-9]\d*)(?:\.\d+)?(?:[eE][+-]?\d+)?)/.exec(raw.slice(position));
    need(token); position += token[0].length;
    if (token[0] === 'true') return true;
    if (token[0] === 'false') return false;
    if (token[0] === 'null') return null;
    const number = Number(token[0]); need(Number.isFinite(number));
    return /^-?\d+$/.test(token[0]) && !Number.isSafeInteger(number) ? BigInt(token[0]) : number;
  }
  const result = value(0); whitespace();
  need(position === raw.length && (record(result) || allowArray && Array.isArray(result)));
  return result;
}

const unchanged = (before, after) => ['dev', 'ino', 'mode', 'uid', 'gid', 'nlink', 'size', 'mtimeNs', 'ctimeNs']
  .every(key => before[key] === after[key]);
const directoryIdentity = info => ({ device: String(info.dev), inode: String(info.ino), mode: String(info.mode), uid: String(info.uid), gid: String(info.gid) });

async function pinParents(filename, directories) {
  const names = []; let parent = path.posix.dirname(filename);
  for (;;) { names.unshift(parent); if (parent === '/') break; parent = path.posix.dirname(parent); }
  for (const name of names) {
    const info = await fs.lstat(name, { bigint: true });
    need(info.isDirectory() && !info.isSymbolicLink() && info.uid === 0n && info.gid === 0n && (info.mode & 0o022n) === 0n);
    if ([WORK, ROOT, TOOL, UPGRADE_TOOL].includes(name)) need((info.mode & 0o7777n) === 0o700n);
    const identity = directoryIdentity(info);
    if (directories.has(name)) need(same(directories.get(name), identity)); else directories.set(name, identity);
  }
}

async function protectedFile(filename, expectedSHA, directories, json = true, maximum = JSON_LIMIT, modes = [0o600n], expectedIdentity) {
  need(ownedPath(filename) && digest(expectedSHA));
  await pinParents(filename, directories);
  need(await fs.realpath(filename) === filename);
  const before = await fs.lstat(filename, { bigint: true });
  need(before.isFile() && !before.isSymbolicLink() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
    modes.includes(before.mode & 0o7777n) && before.size > 0n && before.size <= BigInt(maximum));
  if (expectedIdentity) need(unchanged(expectedIdentity, before));
  const handle = await fs.open(filename, constants.O_RDONLY | constants.O_NOFOLLOW);
  const bytes = Buffer.alloc(65536), chunks = [], sha = createHash('sha256');
  try {
    need(unchanged(before, await handle.stat({ bigint: true })));
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= maximum); sha.update(bytes.subarray(0, part.bytesRead));
      if (json) chunks.push(Buffer.from(bytes.subarray(0, part.bytesRead)));
    }
    need(size === Number(before.size) && sha.digest('hex') === expectedSHA &&
      unchanged(before, await handle.stat({ bigint: true })) && unchanged(before, await fs.lstat(filename, { bigint: true })) &&
      await fs.realpath(filename) === filename);
    await pinParents(filename, directories);
    let value;
    if (json) {
      const raw = Buffer.concat(chunks);
      try { value = parseSource55JSON(new TextDecoder('utf-8', { fatal: true }).decode(raw), true); } finally { raw.fill(0); }
    }
    return { path: filename, sha256: expectedSHA, value, identity: before, modes, maximum };
  } finally { bytes.fill(0); for (const chunk of chunks) chunk.fill(0); await handle.close(); }
}

/** Read only the upgrade, fixed final-history after or fresh-before snapshot without numeric coercion. */
export async function readLibraryChangedSource55Snapshot(input, key) {
  try {
    validateLibraryChangedSource55Input(input);
    need(key === 'current_snapshot' || key === 'history_after_snapshot' || key === 'before_snapshot');
    need(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 &&
      SELF === TOOL + '/client-library-changed-source55-fixture.mjs');
    const item = key === 'history_after_snapshot' ? input.authority.history[4].after_snapshot : input.authority[key];
    const file = await protectedFile(item.path, item.sha256, new Map(), true, SNAPSHOT_LIMIT, [0o600n]);
    need(record(file.value));
    return file.value;
  } catch (error) {
    throw Object.assign(new Error('library_changed_source55_snapshot_read_failed'),
      { diagnostic: libraryChangedSetupDiagnostic(error, 'baseline') });
  }
}

async function procText(filename, maximum = 65536) {
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(maximum + 1);
  try {
    let size = 0;
    while (size < bytes.length) {
      const part = await handle.read(bytes, size, bytes.length - size, size);
      if (!part.bytesRead) break; size += part.bytesRead;
    }
    need(size <= maximum);
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes.subarray(0, size));
  } finally { bytes.fill(0); await handle.close(); }
}

async function checkProcess(expected) {
  const raw = await procText(`/proc/${expected.pid}/stat`), end = raw.lastIndexOf(')');
  need(raw.startsWith(`${expected.pid} (`) && end > 0);
  const fields = raw.slice(end + 1).trim().split(/\s+/);
  need(fields[19] === String(expected.start_ticks) && !['Z', 'X', 'x'].includes(fields[0]));
}

async function hashExecutable(expected, identities) {
  const filename = `/proc/${expected.pid}/exe`;
  need(await fs.readlink(filename) === expected.executable);
  const handle = await fs.open(filename, constants.O_RDONLY), bytes = Buffer.alloc(65536), sha = createHash('sha256');
  try {
    const before = await handle.stat({ bigint: true });
    need(before.isFile() && before.uid === 0n && before.gid === 0n && before.nlink === 1n &&
      (before.mode & 0o7777n) === 0o755n && before.size > 0n && before.size <= 128n * 1024n * 1024n);
    if (identities.has(filename)) need(unchanged(identities.get(filename), before)); else identities.set(filename, before);
    let size = 0;
    for (;;) {
      const part = await handle.read(bytes, 0, bytes.length, size);
      if (!part.bytesRead) break;
      size += part.bytesRead; need(size <= 128 * 1024 * 1024); sha.update(bytes.subarray(0, part.bytesRead));
    }
    need(size === Number(before.size) && sha.digest('hex') === expected.sha256 &&
      unchanged(before, await handle.stat({ bigint: true })) && await fs.readlink(filename) === expected.executable);
  } finally { bytes.fill(0); await handle.close(); }
}

export function validateSource55ProxyArguments(argv) {
  need(Array.isArray(argv) && argv.length >= 3 && argv.length <= 32 && argv.every(value => text(value, 4096)));
  const start = argv.findIndex(value => value.startsWith('--'));
  need(start > 0 && start <= 4 && argv.slice(0, start).filter(value => value.startsWith('/opt/goby-test/') &&
    path.posix.normalize(value) === value && path.posix.basename(value) === 'client-acceptance-proxy.py').length === 1);
  const allowed = new Set(['reference-pid', 'reference-start-ticks', 'reference-sha256', 'reference-port', 'reference-listen',
    'goby-listen', 'goby-port', 'idle-seconds', 'status-file']);
  const options = new Map();
  for (let index = start; index < argv.length; index += 1) {
    const matched = /^--([a-z0-9-]+)(?:=(.*))?$/.exec(argv[index]);
    need(matched && allowed.has(matched[1]) && !options.has(matched[1]));
    const value = matched[2] ?? argv[++index]; need(text(value, 4096) && !value.startsWith('--'));
    options.set(matched[1], value);
  }
  // Reference options are opaque launch arguments. Never resolve their values.
  need(options.get('status-file') === PINS.proxy.path &&
    (!options.has('goby-listen') || options.get('goby-listen') === '18196') &&
    (!options.has('goby-port') || options.get('goby-port') === '18198'));
  if (options.has('idle-seconds')) need(/^\d+$/.test(options.get('idle-seconds')) &&
    Number(options.get('idle-seconds')) >= 30 && Number(options.get('idle-seconds')) <= 3600);
  return true;
}

async function socketOwners(pid) {
  const names = await fs.readdir(`/proc/${pid}/fd`); need(names.length <= 4096);
  const result = new Set();
  for (const name of names) {
    need(/^\d+$/.test(name));
    try { const match = /^socket:\[(\d+)\]$/.exec(await fs.readlink(`/proc/${pid}/fd/${name}`)); if (match) result.add(match[1]); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
  return result;
}

async function pinListeners(candidate) {
  const bindings = [[18196, PROXY_PROCESS.pid], [18198, candidate.process.pid]], selected = [];
  for (const table of ['tcp', 'tcp6']) {
    const raw = await procText(`/proc/self/net/${table}`, JSON_LIMIT);
    for (const line of raw.trim().split('\n').slice(1)) {
      const fields = line.trim().split(/\s+/);
      if (fields[3] === '0A' && bindings.some(([port]) => fields[1]?.endsWith(':' + port.toString(16).toUpperCase())))
        selected.push({ table, address: fields[1], inode: fields[9] });
    }
  }
  for (const [port, pid] of bindings) {
    const suffix = ':' + port.toString(16).toUpperCase(), matches = selected.filter(row => row.address.endsWith(suffix));
    need(matches.length === 1 && matches[0].table === 'tcp' && matches[0].address === '0100007F' + suffix &&
      /^\d+$/.test(matches[0].inode) && (await socketOwners(pid)).has(matches[0].inode));
  }
}

export async function loadLibraryChangedSource55Fixture(options = {}) {
  let phase = 'input';
  try {
    need(exact(options, ['input', 'inputSHA256']) && digest(options.inputSHA256));
    validateLibraryChangedSource55Input(options.input);
    need(process.platform === 'linux' && process.getuid?.() === 0 && process.getgid?.() === 0 &&
      SELF === TOOL + '/client-library-changed-source55-fixture.mjs');
    const directories = new Map(), pins = [], executableIdentities = new Map();
    async function read(value, json = true, maximum = JSON_LIMIT, modes = [0o600n]) {
      const file = await protectedFile(value.path, value.sha256, directories, json, maximum, modes);
      const { value: ignored, ...pin } = file; pins.push(pin); return file.value;
    }
    const input = await read({ path: ROOT + '/input.json', sha256: options.inputSHA256 });
    need(same(input, options.input)); validateLibraryChangedSource55Input(input);
    const candidate = input.candidate, authority = input.authority, scope = upgradeScope(authority);
    const candidateProcess = { ...candidate.process, executable: '/opt/goby-client-m3e/goby', sha256: candidate.binary_sha256,
      uid: 995, cgroup: '/system.slice/goby-client-m3e.service', invocation: candidate.invocation_id };
    phase = 'sources';
    for (const [filename, digestValue] of Object.entries(input.source_closure))
      await read({ path: filename, sha256: digestValue }, false, JSON_LIMIT, [0o600n, 0o644n, 0o700n, 0o755n]);
    phase = 'authority';
    const intent = await read(authority.upgrade_intent);
    validateUpgradeIntent(input, intent);
    const history = [];
    for (const entry of authority.history) {
      const historical = historyScope(entry.version);
      history.push({ version: entry.version, input: await read(entry.input), browser_report: await read(entry.browser_report),
        controller_report: await read(entry.controller_report), terminal: await read(entry.terminal),
        before: await read(entry.before_snapshot, true, SNAPSHOT_LIMIT), after: await read(entry.after_snapshot, true, SNAPSHOT_LIMIT),
        independent: await read(historical.terminalPins.independent_snapshot, true, SNAPSHOT_LIMIT),
        ...(entry.version === 6 ? { discovery: await read(HISTORY_V6_DISCOVERY.accepted), native_identity: await read(HISTORY_V6_NATIVE_PIN) } : {}) });
    }
    const documents = {
      state: await read({ path: STATE, sha256: candidate.state_sha256 }),
      beforeState: await read({ path: scope.output + '/before-state.json', sha256: PREVIOUS.state_sha256 }), intent,
      report: await read(authority.upgrade_report), attestation: await read(authority.upgrade_attestation),
      publication: await read(intent.source.publication), catalog: await read(PINS.catalog, true, SNAPSHOT_LIMIT, [0o600n]),
      current: await read(authority.current_snapshot, true, SNAPSHOT_LIMIT), before: await read(authority.before_snapshot, true, SNAPSHOT_LIMIT),
      history,
      profileReceipt: await read(FIXTURES.profile_receipt), profileReport: await read(FIXTURES.profile_report),
      profileInspection: await read(FIXTURES.profile_inspection), musicChain: await read(FIXTURES.music_chain),
      musicScanReceipt: await read(FIXTURES.music_scan_receipt), proxy: await read(PINS.proxy),
    };
    validateLibraryChangedSource55Documents(input, documents);
    const work = directories.get(WORK);
    need(work && String(documents.state.work_identity.device) === work.device && String(documents.state.work_identity.inode) === work.inode);
    phase = 'private_inputs';
    validateLibraryChangedSource55Viewer(await read(input.actor.credentials, true, 16384));
    await read({ path: WORK + '/runtime.env', sha256: candidate.runtime_sha256 }, false, 65536);
    await read({ path: SOURCE + '/backup-source-inputs.json', sha256: SOURCE_SHA }, false, JSON_LIMIT, [0o600n]);
    const evidence = freeze({ schema: 28, source: 'source-attempt-55', process: { ...candidate.process },
      binary_sha256: candidate.binary_sha256, fixture_state_sha256: candidate.state_sha256,
      source_manifest_sha256: SOURCE_SHA, catalog_sha256: CATALOG_SHA, publication: candidate.publication,
      candidate_invocation_id: candidate.invocation_id, input_sha256: options.inputSHA256,
      source_closure_sha256: hash(canonical(input.source_closure)), authority: { ...input.authority },
      history: HISTORY_ENTRIES.map(entry => ({ version: entry.version, status: 'failed_scope_sealed', terminal: entry.terminal,
        after_snapshot: entry.after_snapshot, retained_additions: entry.version === 6
          ? { sessions: 2, devices: 1, activity_entries: 4, metadata_revision: 0 }
          : { sessions: 1, devices: 1, activity_entries: 2, metadata_revision: 0 } })),
      historical_anchors: FIXTURES, account_id: USER, credentials_sha256: input.actor.credentials.sha256,
      proxy_process: { ...PROXY_PROCESS }, primary_process: { pid: PRIMARY.pid, start_ticks: PRIMARY.start_ticks, boot_id: BOOT },
      boundary: 'Read-only Goby files, source closure, process lifetimes and two listener owners; live database deltas belong to the controller ledger' });
    let checks = 0;
    async function assertPinned() {
      try {
        for (const pin of pins) await protectedFile(pin.path, pin.sha256, directories, false, pin.maximum, pin.modes, pin.identity);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        const network = await fs.readlink('/proc/self/ns/net');
        need(network === await fs.readlink('/proc/1/ns/net'));
        const processes = [candidateProcess, PRIMARY, PROXY_PROCESS];
        for (const expected of processes) {
          await checkProcess(expected); need(await fs.readlink(`/proc/${expected.pid}/ns/net`) === network);
        }
        for (const expected of [candidateProcess, PRIMARY]) {
          need((await fs.stat(`/proc/${expected.pid}`, { bigint: true })).uid === BigInt(expected.uid) &&
            (await procText(`/proc/${expected.pid}/cgroup`)).trim() === '0::' + expected.cgroup &&
            await procText(`/proc/${expected.pid}/cmdline`) === expected.executable + '\0');
          const environment = await procText(`/proc/${expected.pid}/environ`);
          need(environment.endsWith('\0') && environment.slice(0, -1).split('\0')
            .filter(value => value.startsWith('INVOCATION_ID=')).join('') === 'INVOCATION_ID=' + expected.invocation);
          await hashExecutable(expected, executableIdentities);
        }
        const argv = await procText(`/proc/${PROXY_PROCESS.pid}/cmdline`); need(argv.endsWith('\0'));
        validateSource55ProxyArguments(argv.slice(0, -1).split('\0'));
        await pinListeners(candidate);
        for (const expected of processes) await checkProcess(expected);
        need((await procText('/proc/sys/kernel/random/boot_id')).trim() === BOOT);
        for (const pin of pins) await pinParents(pin.path, directories);
        return { phase: 'pinned', check: ++checks, fixture_state_sha256: candidate.state_sha256 };
      } catch (error) {
        throw Object.assign(new Error('library_changed_source55_live_pin_failed'),
          { diagnostic: libraryChangedSetupDiagnostic(error, 'initial_pin') });
      }
    }
    phase = 'initial_pin'; await assertPinned();
    return Object.freeze({ serverId: SERVER, evidence, assertPinned });
  } catch (error) {
    throw Object.assign(new Error('library_changed_source55_' + phase + '_failed'),
      { phase, diagnostic: libraryChangedSetupDiagnostic(error, phase) });
  }
}
