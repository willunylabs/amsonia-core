DROP FUNCTION IF EXISTS amsonia.rename_organization(TEXT, TEXT, BIGINT);
DROP FUNCTION IF EXISTS amsonia.organization_settings();
ALTER TABLE amsonia.tenants DROP COLUMN settings_version;
