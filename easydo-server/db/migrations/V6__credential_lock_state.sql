ALTER TABLE `credentials`
  ADD COLUMN `lock_state` varchar(32) NOT NULL DEFAULT 'unlocked' AFTER `encrypted_payload`,
  ADD KEY `idx_credentials_lock_state` (`lock_state`);
