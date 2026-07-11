// name: MarkTall :exec
MATCH (p:Person) WHERE p.height >= $minHeight SET p.tall = true
