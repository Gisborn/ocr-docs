#!/bin/bash
# Быстрое обновление frontend cabinet без пересборки Go бинарника
# Использование: ./scripts/update-cabinet-frontend.sh [container_name]

CONTAINER=${1:-api-scan-cabinet}

echo "=== Updating frontend in $CONTAINER ==="

# Копируем обновленные статические файлы прямо в контейнер
docker cp ./services/cabinet/pages/index.html $CONTAINER:/root/pages/index.html
docker cp ./docs/legal $CONTAINER:/root/docs/legal

echo "=== Restarting container ==="
docker restart $CONTAINER

echo "=== Done ==="
