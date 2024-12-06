# Bevoegdheid API Service
--------------------

Simple REST API build on top of KVK Bevoegdheden lib. 

## Overview

This service provides an API for interacting with the Kamer van Koophandel (KvK) to verify legal authority (`bevoegdheid`) of individuals and companies based on the provided identity and KVK registration number (`kvkNummer`). The API supports:
- Fetching signatory rights for individuals.
- Retrieving legal person information and certificates.
- Handling cached responses for efficiency.

## Features

- **Company Certificate and LPID Retrieval**: Provides certificates containing legal and registration information for companies.
- **Signatory Right Verification**: Validates whether an individual is authorized to represent a legal entity.
- **Rate Limiting**: Implements request limits per user based on short-term and long-term intervals.
- **Caching**: Uses in-memory caching to reduce API calls for frequently requested data.

### 1. **Fetch Legal Person ID (LPID)**
   - **Endpoint**: `GET /api/lpid/{kvkNummer}`
   - **Description**: Retrieves the legal person identifier and name for the specified `kvkNummer`.
   - **Response**:
     - **200 OK**: LPID details.

---

### 2. **Fetch Company Certificate**
   - **Endpoint**: `GET /api/company-certificate/{kvkNummer}`
   - **Description**: Retrieves a detailed company certificate.
   - **Response**:
     - **200 OK**: Certificate information.

---

### 3. **Check Signatory Right**
   - **Endpoint**: `POST /api/signatory-right/{kvkNummer}`
   - **Description**: Verifies if an individual (`identityNP`) has signatory rights for a legal entity.
   - **Request Body**:
     ```json
     {
       "geslachtsnaam": "Jansen",
       "voorvoegselGeslachtsnaam": "",
       "voornamen": "Frans",
       "geboortedatum": "01-01-2000"
     }
     ```
   - **Response**:
     - **200 OK** (Match Found):
       ```json
       {
         "fullName": "Frans Jansen",
         "isAuthorized": "Yes"
       }
       ```
     - **404 Not Found** (No Match):
       ```json
       {
         "error": "No match found or not authorized"
       }
       ```

---

### 4. **Fetch Bevoegdheid Details** (original functionality)
   - **Endpoint**: `POST /api/bevoegdheid/{kvkNummer}`
   - **Description**: Retrieves detailed `bevoegdheid` information for a given `kvkNummer`.
   - **Response**:
     - **200 OK**: Full `bevoegdheidResponse` object.
     - **400/404**: Error details.

---



## Installation

## Setup
Make sure you have cloned the dependency project https://github.com/kvk-innovatie/kvk-bevoegdheden in the same folder as this project (Check for matching branch names).

### Env variables:
SIGNICAT_CLIENTID= <your CLIENTID>
SIGNICAT_CLIENTSECRET= <your CLIENTSECRET>
SIGNICAT_AUTHSERVER_URL=https://api.signicat.com/auth/open/connect
ENABLE_CACHING=true

## To run locally
Use launch.json:

```json
{
    "version": "0.2.0",
    "configurations": [
        {
            "name": "Launch Package",
            "type": "go",
            "request": "launch",
            "mode": "auto",
            "program": "${workspaceRoot}",
            "showLog": true
        }
    ]
}
```