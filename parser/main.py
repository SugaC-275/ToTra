# parser/main.py
import io

from fastapi import FastAPI, File, HTTPException, UploadFile

app = FastAPI()


@app.post("/parse")
async def parse_file(file: UploadFile = File(...)):
    filename = (file.filename or "").lower()
    data = await file.read()

    if filename.endswith(".pdf"):
        text, page_count = _parse_pdf(data)
    elif filename.endswith(".docx"):
        text, page_count = _parse_docx(data)
    elif filename.endswith(".pptx"):
        text, page_count = _parse_pptx(data)
    else:
        raise HTTPException(status_code=400, detail="unsupported format")

    return {"text": text, "page_count": page_count}


def _parse_pdf(data: bytes) -> tuple[str, int]:
    import pdfplumber

    parts = []
    page_count = 0
    with pdfplumber.open(io.BytesIO(data)) as pdf:
        page_count = len(pdf.pages)
        for page in pdf.pages:
            text = page.extract_text()
            if text:
                parts.append(text)
    return "\n".join(parts), page_count


def _parse_docx(data: bytes) -> tuple[str, int]:
    from docx import Document

    doc = Document(io.BytesIO(data))
    text = "\n".join(p.text for p in doc.paragraphs if p.text.strip())
    return text, 1


def _parse_pptx(data: bytes) -> tuple[str, int]:
    from pptx import Presentation

    prs = Presentation(io.BytesIO(data))
    slides = []
    for slide in prs.slides:
        texts = [
            shape.text
            for shape in slide.shapes
            if hasattr(shape, "text") and shape.text.strip()
        ]
        slides.append("\n".join(texts))
    return "\n\n".join(slides), len(prs.slides)
