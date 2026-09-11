// gqlc-dvd1 / ruling-4w5 §6, the edge row. A single-candidate EDGE carried
// unrenamed across the WITH: the sibling of the carried-node row L6, proving the
// edge lane of Part 1's partScope survives the post-swap mining just as the node
// lane does. $p types as AUTHORED.publishedAt's nullable TIMESTAMP.
MATCH (p:Person)-[r:AUTHORED]->(a:Post) WITH p, r WHERE r.publishedAt = $p RETURN p
