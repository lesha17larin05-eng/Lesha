ALTER TABLE audit_log
  DROP CONSTRAINT IF EXISTS audit_log_admin_id_fkey;
ALTER TABLE audit_log
  ADD CONSTRAINT audit_log_admin_id_fkey
    FOREIGN KEY (admin_id) REFERENCES users(id);

ALTER TABLE video_access_log
  DROP CONSTRAINT IF EXISTS video_access_log_user_id_fkey;
ALTER TABLE video_access_log
  ADD CONSTRAINT video_access_log_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id);

ALTER TABLE enrollments
  DROP CONSTRAINT IF EXISTS enrollments_granted_by_admin_id_fkey;
ALTER TABLE enrollments
  ADD CONSTRAINT enrollments_granted_by_admin_id_fkey
    FOREIGN KEY (granted_by_admin_id) REFERENCES users(id);
