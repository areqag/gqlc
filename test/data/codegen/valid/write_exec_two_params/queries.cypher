// name: RemoveRelation :exec
MATCH (a:Person)-[r:KNOWS]->(b:Person) WHERE a.id = $srcId AND b.id = $tgtId DELETE r
