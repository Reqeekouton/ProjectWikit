-- compat: compatible
ALTER TABLE web_usernotification ADD COLUMN site_id bigint REFERENCES web_site (id);

UPDATE web_usernotification n
SET site_id = t.site_id
FROM web_forumthread t
WHERE n.type IN ('new_post_reply', 'new_thread_post', 'forum_mention', 'post_like')
  AND jsonb_typeof(n.meta #> '{thread,id}') = 'number'
  AND t.id = (n.meta #>> '{thread,id}')::bigint;

UPDATE web_usernotification n
SET site_id = a.site_id
FROM web_article a
WHERE n.type = 'new_article_revision'
  AND jsonb_typeof(n.meta #> '{article,uid}') = 'number'
  AND a.id = (n.meta #>> '{article,uid}')::bigint;
