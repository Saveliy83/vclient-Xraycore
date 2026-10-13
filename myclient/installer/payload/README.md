# Installer payload

Файлы `vclient-gui.exe` и `wintun.dll` сюда помещает
`build-installer.ps1`; не добавляйте бинарники в систему контроля версий.
Сборочный скрипт проверяет Authenticode-подпись DLL до копирования и
останавливается, если подпись невалидна или издатель не совпадает с
`-WintunPublisher`.

Получайте официальный `wintun.dll` с https://www.wintun.net/. Перед сборкой
можно проверить копию вручную:

```powershell
Get-AuthenticodeSignature .\assets\wintun.dll |
    Format-List Status,SignerCertificate
```

Ожидаются `Status: Valid` и издатель, соответствующий значению параметра
`-WintunPublisher` (по умолчанию `WireGuard LLC`). Сверьте издателя на своей
копии DLL; если он отличается, задайте подтверждённое значение явно. DLL не
скачивается во время установки и не включается в репозиторий.
