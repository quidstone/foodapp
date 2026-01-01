FROM python:3.11-slim

WORKDIR /app

# Install system dependencies for psycopg2
RUN apt-get update && apt-get install -y \
    postgresql-client \
    libpq-dev \
    gcc \
    && rm -rf /var/lib/apt/lists/*

# Copy requirements and install Python dependencies
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy ETL script and data files
COPY etl_load_data.py .
COPY restaurant_with_menu.json .
COPY users_with_purchase_history.json .

# Make the script executable
RUN chmod +x etl_load_data.py

# Default command (can be overridden)
CMD ["python", "etl_load_data.py"]

