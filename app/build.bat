@echo off
REM ============================================
REM  Bed Mesh Viewer - Universal Build Script
REM ============================================
REM  Usage:
REM    build.bat           - Compile binary only
REM    build.bat swu       - Compile + create SWU packages for all models
REM    build.bat swu       - Compile + create SWU packages for all models
REM
REM  Requirements:
REM    - Go >= 1.21
REM    - 7-Zip (for SWU mode) at C:\Program Files\7-Zip
REM ============================================

setlocal enabledelayedexpansion

set "BUILD_MODE=%~1"
if "%BUILD_MODE%"=="" set "BUILD_MODE=binary"

REM Resolve absolute paths
set "APP_DIR=%~dp0"
for %%I in ("%APP_DIR%..") do set "PROJECT_DIR=%%~fI"
set "DIST_DIR=%PROJECT_DIR%\assets"

echo === Bed Mesh Viewer Build (Windows) ===
echo Mode: %BUILD_MODE%
echo.

REM --- Find Go ---
set "GO_BIN=go"
where go >nul 2>&1
if %ERRORLEVEL% neq 0 (
    if exist "C:\Program Files\Go\bin\go.exe" (
        echo Go found in C:\Program Files\Go
        set "GO_BIN=C:\Program Files\Go\bin\go.exe"
    ) else (
        echo ERROR: Go is not installed!
        echo Download from: https://go.dev/dl/
        exit /b 1
    )
)

REM --- Clean and create assets directory ---
if exist "%DIST_DIR%" rmdir /s /q "%DIST_DIR%"
mkdir "%DIST_DIR%"

REM --- Compile ---
echo [1/3] Compiling bedmesh_viewer...
set GOOS=linux
set GOARCH=arm
set GOARM=7
set CGO_ENABLED=0

"%GO_BIN%" build -ldflags="-s -w" -o "%DIST_DIR%\bedmesh_viewer" "%APP_DIR%main.go"
if %ERRORLEVEL% neq 0 (
    echo ERROR: Compilation failed!
    exit /b 1
)

for %%A in ("%DIST_DIR%\bedmesh_viewer") do echo       Done! Size: %%~zA bytes


REM --- SWU Mode ---
if "%BUILD_MODE%"=="swu" goto :build_swu
goto :skip_swu

:build_swu
echo.
echo [2/3] Creating SWU packages...

set "SEVENZIP=C:\Program Files\7-Zip\7z.exe"
if not exist "%SEVENZIP%" (
    echo ERROR: 7-Zip not found in C:\Program Files\7-Zip
    echo Install 7-Zip from https://www.7-zip.org/
    exit /b 1
)

REM Create temp staging area
set "STAGE=%TEMP%\bedmesh_swu_%RANDOM%"
if exist "%STAGE%" rmdir /s /q "%STAGE%"
mkdir "%STAGE%\update_swu"

REM Copy files to staging
copy /Y "%DIST_DIR%\bedmesh_viewer" "%STAGE%\update_swu\bedmesh_viewer" >nul
copy /Y "%PROJECT_DIR%\swu\update.sh" "%STAGE%\update_swu\update.sh" >nul

REM Create setup.tar.gz using 7z (tar then gzip)
pushd "%STAGE%\update_swu"
"%SEVENZIP%" a -ttar "..\setup.tar" * >nul
popd
"%SEVENZIP%" a -tgzip "%STAGE%\update_swu\setup.tar.gz" "%STAGE%\setup.tar" >nul
del "%STAGE%\setup.tar" 2>nul

REM Calculate MD5 (using certutil)
certutil -hashfile "%STAGE%\update_swu\setup.tar.gz" MD5 > "%STAGE%\md5tmp.txt" 2>nul
for /f "usebackq skip=1 tokens=*" %%i in ("%STAGE%\md5tmp.txt") do (
    set "MD5HASH=%%i"
    goto :got_md5
)
:got_md5
set "MD5HASH=%MD5HASH: =%"
echo %MD5HASH%> "%STAGE%\update_swu\setup.tar.gz.md5"
del "%STAGE%\md5tmp.txt" 2>nul

REM Remove source files from staging (keep only setup.tar.gz and .md5)
del "%STAGE%\update_swu\bedmesh_viewer" 2>nul
del "%STAGE%\update_swu\update.sh" 2>nul

REM Build SWU for each model (encrypted ZIP with model-specific password)
echo       Packing: bedmesh-swu-k3v2.swu (Kobra 3 V2)
pushd "%STAGE%"
"%SEVENZIP%" a -tzip -p"U2FsdGVkX19deTfqpXHZnB5GeyQ/dtlbHjkUnwgCi+w=" -mx0 "%DIST_DIR%\bedmesh-swu-k3v2.swu" update_swu >nul
popd

echo       Packing: bedmesh-swu-k3m.swu (K3 Max)
pushd "%STAGE%"
"%SEVENZIP%" a -tzip -p"4DKXtEGStWHpPgZm8Xna9qluzAI8VJzpOsEIgd8brTLiXs8fLSu3vRx8o7fMf4h6" -mx0 "%DIST_DIR%\bedmesh-swu-k3m.swu" update_swu >nul
popd

echo       Packing: bedmesh-swu-ks1.swu (Kobra S1)
pushd "%STAGE%"
"%SEVENZIP%" a -tzip -p"U2FsdGVkX1+lG6cHmshPLI/LaQr9cZCjA8HZt6Y8qmbB7riY" -mx0 "%DIST_DIR%\bedmesh-swu-ks1.swu" update_swu >nul
popd

echo       Packing: bedmesh-swu-ks1m.swu (Kobra S1 Max)
pushd "%STAGE%"
"%SEVENZIP%" a -tzip -p"U2FsdGVkX1+lG6cHmshPLI/LaQr9cZCjA8HZt6Y8qmbB7riY" -mx0 "%DIST_DIR%\bedmesh-swu-ks1m.swu" update_swu >nul
popd

REM Cleanup staging
rmdir /s /q "%STAGE%" 2>nul

echo       SWU packages ready!

:skip_swu

echo.
echo === Build finished ===
echo.
echo Output files:
dir /b "%DIST_DIR%" 2>nul

endlocal
