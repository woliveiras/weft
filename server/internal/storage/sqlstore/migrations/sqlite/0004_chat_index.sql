CREATE TABLE chat_session_index (
    key TEXT PRIMARY KEY REFERENCES settings(key) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    archived INTEGER NOT NULL,
    activity_ns INTEGER NOT NULL,
    unread INTEGER NOT NULL,
    running INTEGER NOT NULL,
    revision INTEGER NOT NULL,
    summary TEXT NOT NULL
);

CREATE INDEX chat_session_recents ON chat_session_index(owner, archived, activity_ns DESC, key);
CREATE INDEX chat_session_watches ON chat_session_index(key) WHERE running > 0;
