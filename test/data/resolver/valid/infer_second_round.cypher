MATCH (h:Hub)-[:SPANS]->(b1)-[:FEEDS]->(b2)-[:ANCHORS]->(t:Tail)
RETURN b1, b2
