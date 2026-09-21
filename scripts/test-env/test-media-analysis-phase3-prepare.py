#!/usr/bin/env python3
"""Remote-only pure preparation identity and budget test source."""

import importlib.util
from pathlib import Path
import unittest


ROOT = Path(__file__).resolve().parent
SPEC = importlib.util.spec_from_file_location("phase3_prepare", ROOT / "media-analysis-phase3-prepare.py")
PREPARE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PREPARE)


def case(identity, episode, variant, content):
    return {"case_id": identity, "series_id": "series", "season_id": "season", "episode_id": episode,
            "variant_group": variant, "source": {"sha256": content}}


class SourceIdentityTests(unittest.TestCase):
    def test_episode_variants_never_inflate_independent_sources(self):
        values = [case("episode-original", "one", "one-original", "hash-one"),
                  case("episode-dub", "one", "one-dub", "hash-two"),
                  case("episode-two", "two", "two-original", "hash-three")]
        result = PREPARE.source_components(values)
        self.assertEqual(result["episode-original"], result["episode-dub"])
        self.assertNotEqual(result["episode-original"], result["episode-two"])

    def test_transitive_content_and_variant_aliases_remain_one_group(self):
        values = [case("a", "one", "shared-variant", "hash-one"),
                  case("b", "two", "shared-variant", "hash-two"),
                  case("c", "three", "other-variant", "hash-two")]
        result = PREPARE.source_components(values)
        self.assertEqual(len(set(result.values())), 1)
        self.assertEqual(result, PREPARE.source_components(list(reversed(values))))

    def test_budget_is_rejected_before_a_copy_or_directory_mutation(self):
        preparer = PREPARE.Preparer.__new__(PREPARE.Preparer)
        preparer.operator = {"max_fixture_allocated_bytes": 8192}
        preparer.generated_bytes = 4096
        with self.assertRaisesRegex(PREPARE.WORK.Failure, "preparation_media_byte_budget"):
            preparer.reserve(8192)
        self.assertEqual(preparer.generated_bytes, 4096)


if __name__ == "__main__":
    unittest.main()
