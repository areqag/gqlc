MATCH (p:Person) WHERE p.name = $x RETURN p.name SKIP $x
