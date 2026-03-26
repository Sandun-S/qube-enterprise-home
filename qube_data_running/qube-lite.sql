-- DROP SCHEMA qube;

CREATE SCHEMA qube AUTHORIZATION "admin";

-- DROP TYPE qube."aal_level";

CREATE TYPE qube."aal_level" AS ENUM (
	'aal1',
	'aal2',
	'aal3');

-- DROP TYPE qube."code_challenge_method";

CREATE TYPE qube."code_challenge_method" AS ENUM (
	's256',
	'plain');

-- DROP TYPE qube."factor_status";

CREATE TYPE qube."factor_status" AS ENUM (
	'unverified',
	'verified');

-- DROP TYPE qube."factor_type";

CREATE TYPE qube."factor_type" AS ENUM (
	'totp',
	'webauthn');

-- DROP TYPE qube."one_time_token_type";

CREATE TYPE qube."one_time_token_type" AS ENUM (
	'confirmation_token',
	'reauthentication_token',
	'recovery_token',
	'email_change_token_new',
	'email_change_token_current',
	'phone_change_token');

-- DROP SEQUENCE qube.command_status_changes_id_seq;

CREATE SEQUENCE qube.command_status_changes_id_seq
	INCREMENT BY 1
	MINVALUE 1
	MAXVALUE 2147483647
	START 1
	CACHE 1
	NO CYCLE;
-- DROP SEQUENCE qube.device_commands_sequence_seq;

CREATE SEQUENCE qube.device_commands_sequence_seq
	INCREMENT BY 1
	MINVALUE 1
	MAXVALUE 2147483647
	START 1
	CACHE 1
	NO CYCLE;
-- DROP SEQUENCE qube.device_status_changes_id_seq;

CREATE SEQUENCE qube.device_status_changes_id_seq
	INCREMENT BY 1
	MINVALUE 1
	MAXVALUE 2147483647
	START 1
	CACHE 1
	NO CYCLE;
-- DROP SEQUENCE qube.pendingusers_id_seq;

CREATE SEQUENCE qube.pendingusers_id_seq
	INCREMENT BY 1
	MINVALUE 1
	MAXVALUE 2147483647
	START 1
	CACHE 1
	NO CYCLE;
-- DROP SEQUENCE qube.users_id_seq;

CREATE SEQUENCE qube.users_id_seq
	INCREMENT BY 1
	MINVALUE 1
	MAXVALUE 2147483647
	START 1
	CACHE 1
	NO CYCLE;-- qube.api_tokens definition

-- Drop table

-- DROP TABLE qube.api_tokens;

CREATE TABLE qube.api_tokens (
	client_id int4 NOT NULL,
	"token" varchar(1000) DEFAULT NULL::character varying NULL,
	suspended bool NOT NULL,
	"name" varchar(100) NOT NULL
);


-- qube.command_status_changes definition

-- Drop table

-- DROP TABLE qube.command_status_changes;

CREATE TABLE qube.command_status_changes (
	id serial4 NOT NULL,
	"sequence" int4 NOT NULL,
	device_id varchar(100) NOT NULL,
	old_status int4 NOT NULL,
	new_status int4 NOT NULL,
	changed_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	processed bool DEFAULT false NULL,
	CONSTRAINT command_status_changes_pkey PRIMARY KEY (id)
);


-- qube.device_commands definition

-- Drop table

-- DROP TABLE qube.device_commands;

CREATE TABLE qube.device_commands (
	"sequence" serial4 NOT NULL,
	device_id varchar(100) NOT NULL,
	command varchar(100) NOT NULL,
	parameters varchar(100) NOT NULL,
	data_file varchar(100) NOT NULL,
	status int4 DEFAULT 0 NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	note varchar(1000) DEFAULT NULL::character varying NULL,
	depend int4 DEFAULT 0 NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	CONSTRAINT device_commands_pkey PRIMARY KEY (sequence)
);
CREATE INDEX idx_device_commands_device_id ON qube.device_commands USING btree (device_id);

-- Table Triggers

create trigger after_command_status_update after
update
    on
    qube.device_commands for each row execute function after_command_status_update();


-- qube.device_status_changes definition

-- Drop table

-- DROP TABLE qube.device_status_changes;

CREATE TABLE qube.device_status_changes (
	id serial4 NOT NULL,
	device_id varchar(255) NOT NULL,
	online_status int4 NOT NULL,
	changed_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	processed bool DEFAULT false NULL,
	CONSTRAINT device_status_changes_pkey PRIMARY KEY (id)
);


-- qube.device_tokens definition

-- Drop table

-- DROP TABLE qube.device_tokens;

CREATE TABLE qube.device_tokens (
	client_id int4 NOT NULL,
	device_id varchar(100) NOT NULL,
	suspended bool NOT NULL
);
CREATE INDEX idx_device_tokens_device_id ON qube.device_tokens USING btree (device_id);


-- qube.devices definition

-- Drop table

-- DROP TABLE qube.devices;

CREATE TABLE qube.devices (
	device_id varchar(100) NOT NULL,
	device_name varchar(100) DEFAULT NULL::character varying NULL,
	eth_mac varchar(20) DEFAULT NULL::character varying NULL,
	eth_ipv4 varchar(20) DEFAULT NULL::character varying NULL,
	eth_ipv6 varchar(50) DEFAULT NULL::character varying NULL,
	wlan_mac varchar(20) DEFAULT NULL::character varying NULL,
	wlan_ipv4 varchar(20) DEFAULT NULL::character varying NULL,
	wlan_ipv6 varchar(50) DEFAULT NULL::character varying NULL,
	wlan_ssid varchar(100) DEFAULT NULL::character varying NULL,
	card_id varchar(50) DEFAULT NULL::character varying NULL,
	card_serial varchar(50) DEFAULT NULL::character varying NULL,
	reg_number varchar(20) DEFAULT NULL::character varying NULL,
	mntn_key varchar(20) DEFAULT NULL::character varying NULL,
	updated_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	arch varchar(20) DEFAULT NULL::character varying NULL,
	online bool DEFAULT false NULL,
	user_id int4 NULL,
	CONSTRAINT devices_pkey PRIMARY KEY (device_id)
);

-- Table Triggers

create trigger after_device_online_update after
update
    on
    qube.devices for each row execute function after_device_online_update();


-- qube.fw_rules definition

-- Drop table

-- DROP TABLE qube.fw_rules;

CREATE TABLE qube.fw_rules (
	device_id varchar(100) NOT NULL,
	protocol varchar(5) NOT NULL,
	network varchar(25) NOT NULL,
	port int4 DEFAULT 0 NULL,
	disabled bool DEFAULT false NULL
);
CREATE INDEX idx_fw_rules_device_id ON qube.fw_rules USING btree (device_id);


-- qube.pendingusers definition

-- Drop table

-- DROP TABLE qube.pendingusers;

CREATE TABLE qube.pendingusers (
	id serial4 NOT NULL,
	email varchar(255) NULL,
	company_name varchar(255) NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	supabase_uid uuid NULL,
	provider varchar(100) NULL,
	device_name varchar(255) NULL,
	device_id varchar(255) NULL,
	reg_number varchar(255) NULL,
	CONSTRAINT pendingusers_pkey PRIMARY KEY (id)
);


-- qube.schema_migrations definition

-- Drop table

-- DROP TABLE qube.schema_migrations;

CREATE TABLE qube.schema_migrations (
	"version" varchar(14) NOT NULL,
	CONSTRAINT schema_migrations_pkey PRIMARY KEY (version)
);
CREATE UNIQUE INDEX schema_migrations_version_idx ON qube.schema_migrations USING btree (version);


-- qube.service_types definition

-- Drop table

-- DROP TABLE qube.service_types;

CREATE TABLE qube.service_types (
	service_type varchar(50) NOT NULL,
	description varchar(1000) DEFAULT NULL::character varying NULL,
	is_active bool DEFAULT true NULL,
	CONSTRAINT service_types_pkey PRIMARY KEY (service_type)
);


-- qube.service_versions definition

-- Drop table

-- DROP TABLE qube.service_versions;

CREATE TABLE qube.service_versions (
	service_type varchar(50) NOT NULL,
	"version" varchar(50) NOT NULL,
	arch varchar(20) NOT NULL,
	service_file varchar(100) NOT NULL,
	config_file varchar(100) NOT NULL,
	has_param_file bool NULL,
	ports varchar(100) DEFAULT NULL::character varying NULL,
	has_folder bool NOT NULL,
	link_to_data bool NOT NULL
);


-- qube.users definition

-- Drop table

-- DROP TABLE qube.users;

CREATE TABLE qube.users (
	id serial4 NOT NULL,
	email varchar(100) NOT NULL,
	company_name varchar(30) DEFAULT NULL::character varying NULL,
	email_verified bool DEFAULT false NULL,
	keycloak_uid uuid NULL,
	last_login timestamp NULL,
	provider varchar(100) NULL,
	CONSTRAINT users_email_key UNIQUE (email),
	CONSTRAINT users_pkey PRIMARY KEY (id),
	CONSTRAINT users_supabase_uid_key UNIQUE (keycloak_uid)
);
CREATE INDEX idx_users_supabase_uid ON qube.users USING btree (keycloak_uid);


-- qube.device_services definition

-- Drop table

-- DROP TABLE qube.device_services;

CREATE TABLE qube.device_services (
	device_id varchar(100) NOT NULL,
	service_type varchar(50) NOT NULL,
	service_name varchar(100) NOT NULL,
	config_file varchar(100) NOT NULL,
	param_file varchar(100) NOT NULL,
	created_at timestamp DEFAULT CURRENT_TIMESTAMP NULL,
	service_version varchar(50) DEFAULT NULL::character varying NULL,
	ports varchar(100) NOT NULL,
	folder_file varchar(100) NOT NULL,
	cmd_seq int4 DEFAULT 0 NULL,
	CONSTRAINT fk_device_services_devices FOREIGN KEY (device_id) REFERENCES qube.devices(device_id),
	CONSTRAINT fk_device_services_service_types FOREIGN KEY (service_type) REFERENCES qube.service_types(service_type)
);
CREATE INDEX idx_device_services_device_id ON qube.device_services USING btree (device_id);
CREATE INDEX idx_device_services_service_type ON qube.device_services USING btree (service_type);



-- DROP FUNCTION qube.after_command_status_update();

CREATE OR REPLACE FUNCTION qube.after_command_status_update()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    -- Check if status changed
    IF OLD.status != NEW.status THEN
        INSERT INTO qube.command_status_changes (sequence, device_id, old_status, new_status, changed_at)
        VALUES (NEW.sequence, NEW.device_id, OLD.status, NEW.status, NOW());
    END IF;
    RETURN NEW;
END;
$function$
;

-- DROP FUNCTION qube.after_device_online_update();

CREATE OR REPLACE FUNCTION qube.after_device_online_update()
 RETURNS trigger
 LANGUAGE plpgsql
AS $function$
BEGIN
    -- Check if online status changed
    IF OLD.online != NEW.online THEN
        INSERT INTO qube.device_status_changes (device_id, online_status, changed_at)
        VALUES (NEW.device_id, NEW.online, NOW());
    END IF;
    RETURN NEW;
END;
$function$
;