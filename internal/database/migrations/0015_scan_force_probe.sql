ALTER TABLE scan_jobs
    ADD COLUMN force_probe boolean NOT NULL DEFAULT false;
