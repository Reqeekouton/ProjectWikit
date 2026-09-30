-- compat: compatible
DELETE FROM auth_group_permissions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM web_user_user_permissions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM web_role_permissions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM web_role_restrictions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM web_rolepermissionsoverride_permissions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM web_rolepermissionsoverride_restrictions WHERE permission_id IN (SELECT id FROM auth_permission WHERE codename = 'view_reported_full_conversation');
DELETE FROM auth_permission WHERE codename = 'view_reported_full_conversation';
