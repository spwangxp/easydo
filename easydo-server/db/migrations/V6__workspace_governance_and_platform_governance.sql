-- Prepare workspace governance kind foundation

ALTER TABLE `workspaces`
  ADD COLUMN `kind` varchar(32) NOT NULL DEFAULT 'normal' AFTER `visibility`,
  ADD KEY `idx_workspaces_kind` (`kind`);

UPDATE `workspaces`
SET `kind` = 'normal'
WHERE `kind` IS NULL OR `kind` = '';
