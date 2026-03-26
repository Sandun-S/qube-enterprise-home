-- qube_schema.ccs_measurements definition

-- Drop table

-- DROP TABLE qube_schema.ccs_measurements;

CREATE TABLE qube_schema.ccs_measurements (
	site varchar(50) NULL,
	"section" varchar(100) NULL,
	device varchar(100) NULL,
	unit_id varchar(100) NULL,
	"label" varchar(100) NULL,
	reading varchar(100) NULL,
	value numeric(18, 2) NULL,
	logged_time timestamptz NULL
);
CREATE INDEX ccs_measurements_idx ON qube_schema.ccs_measurements USING btree (logged_time);
CREATE INDEX idx_ccs_measurements_device_logged_time ON qube_schema.ccs_measurements USING btree (device, logged_time DESC);

-- Table Triggers

create trigger trg_update_sensor_value after
insert
    on
    qube_schema.ccs_measurements for each row execute function qube_schema.update_sensor_latest_value();