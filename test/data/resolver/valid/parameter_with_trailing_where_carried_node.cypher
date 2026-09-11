// gqlc-dvd1 / ruling-4w5 §6 row L6. The ORDINARY WITH-trailing-WHERE shape, and
// the row that decides whether the post-swap mining is affordable: `a` is an
// UNRENAMED carried node, so Part 1's partScope holds it in the same entity lane
// Part 0 did and $p must still type as Post.id's INT. A degradation here
// falsifies the ruling (§9) rather than rebaselining this golden.
MATCH (a:Post) WITH a WHERE a.id = $p RETURN a
