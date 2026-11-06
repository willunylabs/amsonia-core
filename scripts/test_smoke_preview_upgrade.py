"""Guard data preservation checks used by the real archive upgrade smoke test."""

from copy import deepcopy
from pathlib import Path
import runpy
import unittest


verify_upgrade = runpy.run_path(str(Path(__file__).with_name("smoke-preview-upgrade.py")))["verify_upgrade"]


class UpgradePreservationTests(unittest.TestCase):
    def setUp(self):
        self.before = {
            "amsonia.accounts": "2|account-digest",
            "amsonia.audit_events": "1|audit-digest",
            "amsonia.schema_migrations": "10|old-history-digest",
        }
        self.old = [{"version": version, "dirty": False, "checksum": f"checksum-{version}"}
                    for version in range(1, 11)]
        self.after = {
            **self.before,
            "amsonia.schema_migrations": "11|new-history-digest",
            "amsonia.member_password_recoveries": "0|d41d8cd98f00b204e9800998ecf8427e",
        }
        self.new = [*deepcopy(self.old), {"version": 11, "dirty": False, "checksum": "checksum-11"}]

    def test_schema_10_to_11_and_existing_schema(self):
        verify_upgrade(self.before, self.after, self.old, self.new)
        verify_upgrade(self.before, self.before, self.old, self.old)

    def test_data_loss_or_changes_are_rejected(self):
        for name in ("amsonia.accounts", "amsonia.audit_events"):
            for value in (None, "0|d41d8cd98f00b204e9800998ecf8427e", "2|changed-digest"):
                with self.subTest(table=name, value=value):
                    after = {**self.after, name: value}
                    if value is None:
                        del after[name]
                    with self.assertRaises(AssertionError):
                        verify_upgrade(self.before, after, self.old, self.new)

    def test_unexpected_or_nonempty_new_tables_are_rejected(self):
        for name, value in (("amsonia.unexpected", "0|d41d8cd98f00b204e9800998ecf8427e"),
                            ("amsonia.member_password_recoveries", "1|unexpected-record")):
            with self.subTest(table=name), self.assertRaises(AssertionError):
                verify_upgrade(self.before, {**self.after, name: value}, self.old, self.new)

    def test_migration_corruption_is_rejected(self):
        changed = deepcopy(self.new)
        changed[0]["checksum"] = "changed-checksum"
        dirty = deepcopy(self.new)
        dirty[-1]["dirty"] = True
        gap = deepcopy(self.new)
        gap[-1]["version"] = 12
        for migrations in (changed, dirty, gap, self.new[:-2]):
            with self.subTest(migrations=migrations), self.assertRaises(AssertionError):
                verify_upgrade(self.before, self.after, self.old, migrations)


if __name__ == "__main__":
    unittest.main()
