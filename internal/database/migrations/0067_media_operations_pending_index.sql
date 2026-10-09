-- Keep polling proportional to pending work instead of retained operation
-- history. The queued-only index remains necessary for admission counts.
CREATE INDEX media_operations_pending_order_idx
    ON media_operations ((cancel_requested_at IS NOT NULL) DESC, created_at, id)
    WHERE worker_token = ''
      AND (state IN ('queued', 'applying')
           OR (state IN ('ready', 'interrupted')
               AND cancel_requested_at IS NOT NULL));
