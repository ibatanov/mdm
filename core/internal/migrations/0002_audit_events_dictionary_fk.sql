ALTER TABLE audit_events
	DROP CONSTRAINT IF EXISTS audit_events_dictionary_fk;

ALTER TABLE audit_events
	ADD CONSTRAINT audit_events_dictionary_fk
		FOREIGN KEY (dictionary_id) REFERENCES dictionaries (id) ON DELETE SET NULL;
