-- AdGuard Home only reports whether it can update itself while a newer
-- version is on offer, so remember the last real answer.
ALTER TABLE upgrade_status ADD COLUMN autoupdate_known INTEGER NOT NULL DEFAULT 0;
UPDATE upgrade_status SET autoupdate_known = 1 WHERE can_autoupdate = 1;
