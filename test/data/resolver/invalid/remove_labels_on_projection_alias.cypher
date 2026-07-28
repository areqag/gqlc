MATCH (n:Person) WITH count(n) AS c REMOVE c:Foo
