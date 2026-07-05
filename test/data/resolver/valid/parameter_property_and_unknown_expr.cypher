MATCH (p:Person) WHERE p.age = $x RETURN p.age + $x AS bumped
