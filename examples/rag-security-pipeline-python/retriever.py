def retrieve_docs(query: str):
    # Simulated vector search
    with open("data.txt") as f:
        docs = f.read()
    return docs
