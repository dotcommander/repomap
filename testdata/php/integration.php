<?php

namespace App\Domain\Order;

use App\Contracts\HasLabel;
use App\Contracts\Identifiable;
use Psr\Log\LoggerInterface;

/**
 * Manages order lifecycle and persistence.
 */
class OrderService extends BaseService implements Identifiable
{
    const VERSION = '2.0';

    /**
     * Initialise the service with its dependencies.
     */
    public function __construct(
        public readonly LoggerInterface $logger,
        public readonly string $queueName,
        string $internalSecret,
    ) {}

    /**
     * Persist an order and emit a queued event.
     */
    public function save(Order $order, bool $notify = true): void {}

    /**
     * Load and validate the order by its identifier.
     */
    protected function loadById(int $id): ?Order
    {
        return null;
    }
}

/**
 * Represents the current status of an order.
 */
enum OrderStatus: string implements HasLabel
{
    /** Order has been placed but not yet confirmed. */
    case Pending = 'pending';
    case Confirmed = 'confirmed';
    case Cancelled = 'cancelled';

    public function label(): string
    {
        return match($this) {
            OrderStatus::Pending   => 'Pending',
            OrderStatus::Confirmed => 'Confirmed',
            OrderStatus::Cancelled => 'Cancelled',
        };
    }
}

/**
 * Adds structured logging to any class that uses it.
 */
trait Loggable
{
    public function logInfo(string $message): void {}
}

/**
 * Defines the read contract for order repositories.
 */
interface OrderRepositoryInterface
{
    public function findById(int $id): ?Order;
    public function findAll(): array;
}

/**
 * Find all orders matching the given status.
 */
function findOrdersByStatus(OrderStatus $status, int $limit = 50): array
{
    return [];
}
