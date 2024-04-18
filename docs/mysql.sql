-- Set up mysql database.

CREATE DATABASE IF NOT EXISTS minipush;

USE minipush;

CREATE TABLE IF NOT EXISTS `events` (
  topic_offset BIGINT UNSIGNED NOT NULL PRIMARY KEY, -- offset of kafka topic.
  create_time DATETIME(3) NOT NULL,
  payload VARCHAR(4096) NOT NULL DEFAULT ''
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `user_events` (
  uid INT(11) UNSIGNED NOT NULL, -- recepient user id
  seq INT(11) UNSIGNED NOT NULL, -- user event sequence
  create_time DATETIME(3) NOT NULL,
  read_state SMALLINT(1) NOT NULL DEFAULT 0, -- 0: unread, 1: read
  topic_offset BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY `idx_uid_seq` (`uid`, `seq`),
  KEY `idx_create_time` (`create_time`),
  FOREIGN KEY (topic_offset) REFERENCES events (topic_offset) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE IF NOT EXISTS `user_seq` (
  uid INT(11) UNSIGNED NOT NULL PRIMARY KEY, -- recepient user id
  seq INT(11) UNSIGNED NOT NULL -- user event sequence
) ENGINE=InnoDB;
