-- compat: compatible
UPDATE web_user u
SET wikidot_user_id = substring(u.wikidot_username::text from '^已删除-([0-9]+)$')::bigint
WHERE u.type = 'wikidot'
  AND u.wikidot_user_id IS NULL
  AND u.wikidot_username::text ~ '^已删除-[0-9]+$'
  AND NOT EXISTS (
    SELECT 1 FROM web_user o
    WHERE o.wikidot_user_id = substring(u.wikidot_username::text from '^已删除-([0-9]+)$')::bigint);

UPDATE web_user u
SET display_name = coalesce(nullif(u.display_name, ''), u.wikidot_username::text),
    wikidot_username = 'deleted-' || substring(u.wikidot_username::text from '^已删除-([0-9]+)$')
WHERE u.type = 'wikidot'
  AND u.wikidot_username::text ~ '^已删除-[0-9]+$'
  AND NOT EXISTS (
    SELECT 1 FROM web_user o
    WHERE lower(o.wikidot_username::text) = 'deleted-' || substring(u.wikidot_username::text from '^已删除-([0-9]+)$'));
